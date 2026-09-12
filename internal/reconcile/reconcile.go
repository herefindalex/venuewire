package reconcile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/orderstate"
	"github.com/herefindalex/venuewire/internal/rest"
)

type ExchangeReader interface {
	OpenOrders(context.Context) ([]domain.Order, error)
	RecentOrders(context.Context) ([]domain.Order, error)
	Executions(context.Context) ([]domain.Execution, error)
}

type Discrepancy struct {
	Kind         string             `json:"kind"`
	OrderID      string             `json:"orderId,omitempty"`
	OrderLinkID  string             `json:"orderLinkId,omitempty"`
	LocalStatus  domain.OrderStatus `json:"localStatus,omitempty"`
	RemoteStatus domain.OrderStatus `json:"remoteStatus,omitempty"`
	Action       string             `json:"action"`
	Error        string             `json:"error,omitempty"`
}

type Report struct {
	StartedAt     time.Time     `json:"startedAt"`
	FinishedAt    time.Time     `json:"finishedAt"`
	OpenFetched   int           `json:"openFetched"`
	RecentFetched int           `json:"recentFetched"`
	ExecFetched   int           `json:"executionsFetched"`
	Discrepancies []Discrepancy `json:"discrepancies"`
}

type Reconciler struct {
	State    *orderstate.Service
	Exchange ExchangeReader
}

func (r Reconciler) Reconcile(ctx context.Context) (Report, error) {
	report := Report{StartedAt: time.Now().UTC(), Discrepancies: []Discrepancy{}}
	if r.State == nil || r.Exchange == nil {
		return report, fmt.Errorf("reconciler requires state and exchange reader")
	}
	openOrders, err := r.Exchange.OpenOrders(ctx)
	if err != nil {
		return report, fmt.Errorf("fetch open orders: %w", err)
	}
	recentOrders, err := r.Exchange.RecentOrders(ctx)
	if err != nil {
		return report, fmt.Errorf("fetch recent orders: %w", err)
	}
	executions, err := r.Exchange.Executions(ctx)
	if err != nil {
		return report, fmt.Errorf("fetch executions: %w", err)
	}
	report.OpenFetched, report.RecentFetched, report.ExecFetched = len(openOrders), len(recentOrders), len(executions)
	before := r.State.Snapshot()
	remote := deduplicateOrders(append(append([]domain.Order{}, openOrders...), recentOrders...))
	matched := make(map[string]bool)
	for _, order := range remote {
		key, local, found := findSnapshotOrder(before, order.OrderID, order.OrderLinkID)
		if found {
			matched[key] = true
		}
		if !found || local.Status != order.Status || local.CumFilledQty != order.CumFilledQty || local.OrderID != order.OrderID {
			d := Discrepancy{Kind: "order_state", OrderID: order.OrderID, OrderLinkID: order.OrderLinkID, RemoteStatus: order.Status, Action: "repaired_from_REST"}
			if found {
				d.LocalStatus = local.Status
			} else {
				d.Kind = "unknown_exchange_order"
				d.Action = "imported_from_REST"
			}
			if err := r.State.ApplyOrder(ctx, order); err != nil {
				d.Action = "left_visible"
				d.Error = err.Error()
				report.Discrepancies = append(report.Discrepancies, d)
				continue
			}
			report.Discrepancies = append(report.Discrepancies, d)
		}
	}
	sort.SliceStable(executions, func(i, j int) bool { return executions[i].ExchangeTime.Before(executions[j].ExchangeTime) })
	knownExec := before.Executions
	for _, execution := range executions {
		if _, exists := knownExec[execution.ExecutionID]; exists {
			continue
		}
		d := Discrepancy{Kind: "missing_execution", OrderID: execution.OrderID, OrderLinkID: execution.OrderLinkID, Action: "applied_execution"}
		_, _, reflectedByRemote := findDomainOrder(remote, execution.OrderID, execution.OrderLinkID)
		var applyErr error
		if reflectedByRemote {
			d.Action = "recorded_execution_REST_order_already_authoritative"
			applyErr = r.State.RecordExecution(ctx, execution)
		} else {
			applyErr = r.State.ApplyExecution(ctx, execution)
		}
		if applyErr != nil {
			d.Action = "left_visible"
			d.Error = applyErr.Error()
		}
		report.Discrepancies = append(report.Discrepancies, d)
	}
	after := r.State.Snapshot()
	for key, local := range before.Orders {
		if matched[key] || isTerminal(after.Orders[key].Status) {
			continue
		}
		if _, _, found := findDomainOrder(remote, local.OrderID, local.OrderLinkID); !found {
			report.Discrepancies = append(report.Discrepancies, Discrepancy{Kind: "local_order_absent_remotely", OrderID: local.OrderID, OrderLinkID: local.OrderLinkID, LocalStatus: local.Status, Action: "left_visible_ambiguous"})
		}
	}
	report.FinishedAt = time.Now().UTC()
	return report, nil
}

func deduplicateOrders(orders []domain.Order) []domain.Order {
	result := make([]domain.Order, 0, len(orders))
	index := make(map[string]int)
	for _, order := range orders {
		key := order.OrderID
		if key == "" {
			key = order.OrderLinkID
		}
		if position, exists := index[key]; exists {
			if result[position].UpdatedAt.Before(order.UpdatedAt) {
				result[position] = order
			}
			continue
		}
		index[key] = len(result)
		result = append(result, order)
	}
	return result
}

func findSnapshotOrder(snapshot orderstate.Snapshot, orderID, orderLinkID string) (string, domain.Order, bool) {
	if orderLinkID != "" {
		if order, ok := snapshot.Orders[orderLinkID]; ok {
			return orderLinkID, order, true
		}
	}
	for key, order := range snapshot.Orders {
		if orderID != "" && order.OrderID == orderID {
			return key, order, true
		}
	}
	return "", domain.Order{}, false
}

func findDomainOrder(orders []domain.Order, orderID, orderLinkID string) (int, domain.Order, bool) {
	for i, order := range orders {
		if (orderID != "" && order.OrderID == orderID) || (orderLinkID != "" && order.OrderLinkID == orderLinkID) {
			return i, order, true
		}
	}
	return 0, domain.Order{}, false
}

func isTerminal(status domain.OrderStatus) bool {
	return status == domain.OrderStatusFilled || status == domain.OrderStatusCancelled || status == domain.OrderStatusRejected
}

type RESTReader struct {
	Client   *rest.Client
	Category string
	Symbols  []string
}

func (r RESTReader) OpenOrders(ctx context.Context) ([]domain.Order, error) {
	value := 0
	return r.orders(ctx, &value)
}
func (r RESTReader) RecentOrders(ctx context.Context) ([]domain.Order, error) {
	value := 2
	return r.orders(ctx, &value)
}
func (r RESTReader) orders(ctx context.Context, openOnly *int) ([]domain.Order, error) {
	var result []domain.Order
	for _, symbol := range r.Symbols {
		cursor := ""
		seen := map[string]bool{}
		for {
			page, _, err := r.Client.OrdersPage(ctx, r.Category, symbol, "", "", openOnly, cursor)
			if err != nil {
				return nil, err
			}
			for _, order := range page.List {
				result = append(result, restOrder(r.Category, order))
			}
			if page.NextPageCursor == "" {
				break
			}
			if seen[page.NextPageCursor] {
				return nil, errors.New("Bybit order pagination cursor repeated")
			}
			seen[page.NextPageCursor] = true
			cursor = page.NextPageCursor
		}
	}
	return result, nil
}
func (r RESTReader) Executions(ctx context.Context) ([]domain.Execution, error) {
	var result []domain.Execution
	for _, symbol := range r.Symbols {
		cursor := ""
		seen := map[string]bool{}
		for {
			page, _, err := r.Client.ExecutionsPage(ctx, r.Category, symbol, "", "", cursor)
			if err != nil {
				return nil, err
			}
			for _, execution := range page.List {
				result = append(result, restExecution(r.Category, execution))
			}
			if page.NextPageCursor == "" {
				break
			}
			if seen[page.NextPageCursor] {
				return nil, errors.New("Bybit execution pagination cursor repeated")
			}
			seen[page.NextPageCursor] = true
			cursor = page.NextPageCursor
		}
	}
	return result, nil
}

func restOrder(category string, order rest.Order) domain.Order {
	return domain.Order{Exchange: "bybit", Category: category, Symbol: order.Symbol, OrderID: order.OrderID, OrderLinkID: order.OrderLinkID, Side: domain.Side(order.Side), Type: domain.OrderType(order.OrderType), Price: order.Price, Qty: order.Qty, CumFilledQty: order.CumExecQty, AvgFillPrice: order.AvgPrice, Status: domain.NormalizeBybitOrderStatus(order.OrderStatus), RawStatus: order.OrderStatus, CreatedAt: parseMillis(order.CreatedTime), UpdatedAt: parseMillis(order.UpdatedTime)}
}
func restExecution(category string, execution rest.Execution) domain.Execution {
	return domain.Execution{Exchange: "bybit", Category: category, Symbol: execution.Symbol, ExecutionID: execution.ExecID, OrderID: execution.OrderID, OrderLinkID: execution.OrderLinkID, Side: domain.Side(execution.Side), Price: execution.ExecPrice, Qty: execution.ExecQty, ExchangeTime: parseMillis(execution.ExecTime), ReceivedAt: time.Now().UTC()}
}
func parseMillis(value string) time.Time {
	millis, err := strconv.ParseInt(value, 10, 64)
	if err != nil || millis == 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis).UTC()
}
