package deribitreconcile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"bybit/internal/deribit"
	"bybit/internal/domain"
	"bybit/internal/intent"
	"bybit/internal/orderstate"
)

type Queries interface {
	OrdersByLabel(context.Context, string, string) ([]deribit.Order, error)
	TradesByOrder(context.Context, string) ([]deribit.Trade, error)
	TradesByInstrument(context.Context, string, int64, int) (deribit.TradePage, error)
}

type Reconciler struct {
	Queries      Queries
	Intents      intent.Store
	State        *orderstate.Service
	AccountAlias string
	Now          func() time.Time
}
type Report struct {
	PlansChecked              int `json:"plansChecked"`
	Recovered                 int `json:"recovered"`
	NeedsReview               int `json:"needsReview"`
	Unresolved                int `json:"unresolved"`
	OrdersApplied             int `json:"ordersApplied"`
	TradesApplied             int `json:"tradesApplied"`
	DuplicateOrExternalTrades int `json:"duplicateOrExternalTrades"`
	Pages                     int `json:"pages"`
}

type PrivateEventReport struct {
	OrdersApplied             int `json:"ordersApplied"`
	TradesApplied             int `json:"tradesApplied"`
	DuplicateOrExternalTrades int `json:"duplicateOrExternalTrades"`
	PositionsObserved         int `json:"positionsObserved"`
}

func (r *Reconciler) ApplyUserChanges(ctx context.Context, changes deribit.UserChanges) (PrivateEventReport, error) {
	var report PrivateEventReport
	if r.State == nil {
		return report, errors.New("Deribit private-event reducer requires order state")
	}
	labels := make(map[string]string, len(changes.Orders))
	for _, order := range changes.Orders {
		if err := r.applyOrder(ctx, order); err != nil {
			return report, err
		}
		labels[order.OrderID] = order.Label
		report.OrdersApplied++
	}
	for _, trade := range changes.Trades {
		applied, err := r.recordTrade(ctx, trade, labels[trade.OrderID])
		if err != nil {
			return report, err
		}
		if applied {
			report.TradesApplied++
		} else {
			report.DuplicateOrExternalTrades++
		}
	}
	report.PositionsObserved = len(changes.Positions)
	return report, nil
}

func (r *Reconciler) Run(ctx context.Context) (Report, error) {
	return r.run(ctx, false)
}

func (r *Reconciler) RecoverUncertain(ctx context.Context) (Report, error) { return r.run(ctx, true) }

func (r *Reconciler) run(ctx context.Context, uncertainOnly bool) (Report, error) {
	var report Report
	if r.Queries == nil || r.State == nil {
		return report, errors.New("Deribit reconciler dependencies are required")
	}
	plans, err := r.Intents.List(ctx)
	if err != nil {
		return report, err
	}
	instruments := map[string]bool{}
	for _, plan := range plans {
		if plan.Venue != "deribit" {
			continue
		}
		instruments[plan.Instrument] = true
		if plan.Status == intent.StatusPlanned || plan.Status == intent.StatusExpired || plan.Status == intent.StatusRejected {
			continue
		}
		if uncertainOnly && plan.Status != intent.StatusExecuting && plan.Status != intent.StatusOutcomeUnknown && plan.Status != intent.StatusNeedsReview {
			continue
		}
		report.PlansChecked++
		orders, err := r.Queries.OrdersByLabel(ctx, plan.Instrument, plan.ID)
		if err != nil {
			return report, fmt.Errorf("recover intent %s: %w", plan.ID, err)
		}
		switch len(orders) {
		case 0:
			report.Unresolved++
			continue
		case 1:
		default:
			_ = r.Intents.Finish(ctx, plan.ID, intent.StatusNeedsReview, "", "multiple native orders share intent label")
			report.NeedsReview++
			continue
		}
		order := orders[0]
		if plan.NativeOrderID != "" && plan.NativeOrderID != order.OrderID {
			_ = r.Intents.Finish(ctx, plan.ID, intent.StatusNeedsReview, plan.NativeOrderID, "native order ID conflict")
			report.NeedsReview++
			continue
		}
		if err := r.applyOrder(ctx, order); err != nil {
			return report, err
		}
		report.OrdersApplied++
		if plan.Status == intent.StatusExecuting || plan.Status == intent.StatusOutcomeUnknown || plan.Status == intent.StatusNeedsReview {
			if err := r.Intents.Finish(ctx, plan.ID, intent.StatusSubmitted, order.OrderID, ""); err != nil {
				return report, err
			}
			report.Recovered++
		}
		trades, err := r.Queries.TradesByOrder(ctx, order.OrderID)
		if err != nil {
			return report, err
		}
		for _, trade := range trades {
			applied, err := r.recordTrade(ctx, trade, plan.ID)
			if err != nil {
				return report, err
			}
			if applied {
				report.TradesApplied++
			} else {
				report.DuplicateOrExternalTrades++
			}
		}
	}
	if uncertainOnly {
		return report, nil
	}
	for instrument := range instruments {
		if err := r.reconcilePages(ctx, instrument, &report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func (r *Reconciler) applyOrder(ctx context.Context, order deribit.Order) error {
	created := time.UnixMilli(order.CreationTimestamp).UTC()
	updated := time.UnixMilli(order.LastUpdateTimestamp).UTC()
	return r.State.ApplyOrder(ctx, domain.Order{Exchange: "deribit", Environment: "testnet", AccountAlias: r.AccountAlias, Category: "future", Symbol: order.InstrumentName, OrderID: order.OrderID, OrderLinkID: order.Label, IntentID: order.Label, Side: domain.Side(strings.Title(order.Direction)), Type: domain.OrderType(strings.Title(order.OrderType)), Price: order.Price.String(), Qty: order.Amount.String(), CumFilledQty: order.FilledAmount.String(), AvgFillPrice: order.AveragePrice.String(), Status: normalizeStatus(order.OrderState), RawStatus: order.OrderState, CreatedAt: created, UpdatedAt: updated})
}

func normalizeStatus(raw string) domain.OrderStatus {
	switch raw {
	case "open", "untriggered":
		return domain.OrderStatusNew
	case "filled":
		return domain.OrderStatusFilled
	case "cancelled":
		return domain.OrderStatusCancelled
	case "rejected":
		return domain.OrderStatusRejected
	default:
		return domain.OrderStatusUnknown
	}
}

func (r *Reconciler) recordTrade(ctx context.Context, trade deribit.Trade, label string) (bool, error) {
	execution := domain.Execution{Exchange: "deribit", Environment: "testnet", AccountAlias: r.AccountAlias, Category: "future", Symbol: trade.InstrumentName, ExecutionID: trade.TradeID, OrderID: trade.OrderID, OrderLinkID: label, Side: domain.Side(strings.Title(trade.Direction)), Price: trade.Price.String(), Qty: trade.Amount.String(), Fee: trade.Fee.String(), FeeCurrency: trade.FeeCurrency, ExchangeTime: time.UnixMilli(trade.Timestamp).UTC(), ReceivedAt: r.now().UTC()}
	before := len(r.State.Snapshot().Executions)
	err := r.State.RecordExecution(ctx, execution)
	if err != nil && strings.Contains(err.Error(), "references unknown order") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(r.State.Snapshot().Executions) > before, nil
}

func (r *Reconciler) reconcilePages(ctx context.Context, instrument string, report *Report) error {
	key := "deribit|testnet|" + r.AccountAlias + "|" + instrument
	cursor, err := r.Intents.Cursor(ctx, key)
	if err != nil {
		return err
	}
	start := cursor.Timestamp
	for pageNumber := 0; pageNumber < 10; pageNumber++ {
		page, err := r.Queries.TradesByInstrument(ctx, instrument, start, 100)
		if err != nil {
			return err
		}
		report.Pages++
		sort.Slice(page.Trades, func(i, j int) bool {
			if page.Trades[i].Timestamp == page.Trades[j].Timestamp {
				return page.Trades[i].TradeID < page.Trades[j].TradeID
			}
			return page.Trades[i].Timestamp < page.Trades[j].Timestamp
		})
		advanced := false
		for _, trade := range page.Trades {
			if trade.Timestamp < cursor.Timestamp {
				report.DuplicateOrExternalTrades++
				continue
			}
			applied, err := r.recordTrade(ctx, trade, "")
			if err != nil {
				return err
			}
			if applied {
				report.TradesApplied++
			} else {
				report.DuplicateOrExternalTrades++
			}
			candidate := intent.TradeCursor{Timestamp: trade.Timestamp, TradeID: trade.TradeID}
			if candidate.Timestamp > cursor.Timestamp || (candidate.Timestamp == cursor.Timestamp && candidate.TradeID > cursor.TradeID) {
				cursor = candidate
				advanced = true
			}
		}
		if advanced {
			if err := r.Intents.SetCursor(ctx, key, cursor); err != nil {
				return err
			}
			start = cursor.Timestamp
		}
		if !page.HasMore {
			return nil
		}
		if len(page.Trades) == 0 || !advanced {
			return errors.New("trade pagination made no progress")
		}
	}
	return errors.New("trade pagination exceeded bounded page limit")
}
func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
