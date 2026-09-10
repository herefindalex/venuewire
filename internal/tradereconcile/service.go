package tradereconcile

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"venuewire/internal/deribit"
	"venuewire/internal/domain"
	"venuewire/internal/intent"
	"venuewire/internal/quicktrade"
	"venuewire/internal/rest"
)

type BybitReader interface {
	Orders(context.Context, string, string, string, string, *int) ([]rest.Order, rest.ResponseMeta, error)
	Executions(context.Context, string, string, string, string) ([]rest.Execution, rest.ResponseMeta, error)
}

type DeribitReader interface {
	OrderState(context.Context, string) (deribit.Order, error)
	OrdersByLabel(context.Context, string, string) ([]deribit.Order, error)
	TradesByOrder(context.Context, string) ([]deribit.Trade, error)
}

type AccountRefresher interface {
	Refresh(context.Context, domain.Venue) error
}

type Service struct {
	mu       sync.Mutex
	Store    intent.Store
	Bybit    BybitReader
	Deribit  DeribitReader
	Accounts AccountRefresher
	OnEvent  func(string, domain.Venue, string, time.Time)
}

type resolution struct {
	status           intent.TradeStatus
	rawStatus        string
	resultStatus     string
	venueOrderID     string
	filledBaseQty    string
	averagePrice     string
	grossSource      string
	grossDestination string
	fees             []intent.TradeFee
	netDestination   string
	actualSource     string
	fillStatus       string
	feeStatus        string
	definitive       bool
}

func (s *Service) RecheckTrade(ctx context.Context, intentID string, now time.Time) (intent.QuickTrade, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	trade, err := s.Store.GetQuickTrade(ctx, intentID)
	if err != nil {
		return intent.QuickTrade{}, err
	}
	if trade.Status == intent.TradeCreated && !trade.SendAttempted {
		return s.Store.UpdateQuickTrade(ctx, intentID, intent.QuickTradeUpdate{
			Status: intent.TradeRejected, ResultStatus: "NOT_SUBMITTED_AFTER_RESTART",
			LifecycleDetail: "The durable intent was recovered before any venue send attempt.", ClearPublicError: true, Checked: true,
		}, now)
	}

	var resolved resolution
	switch domain.Venue(trade.Quote.Venue) {
	case domain.VenueBybit:
		resolved, err = s.recheckBybit(ctx, trade)
	case domain.VenueDeribit:
		resolved, err = s.recheckDeribit(ctx, trade)
	default:
		err = errors.New("trade has unsupported venue")
	}
	if err != nil {
		return intent.QuickTrade{}, &quicktrade.Error{Code: "VENUE_RECOVERING", PublicMessage: "Venue evidence could not be checked yet.", Err: err}
	}
	update := intent.QuickTradeUpdate{
		RawVenueStatus: resolved.rawStatus, ResultStatus: resolved.resultStatus, VenueOrderID: resolved.venueOrderID,
		FilledBaseQty: resolved.filledBaseQty, AveragePrice: resolved.averagePrice,
		GrossSourceSpent: resolved.grossSource, GrossDestinationReceived: resolved.grossDestination,
		Fees: resolved.fees, NetDestinationReceived: resolved.netDestination, ActualSourceDebit: resolved.actualSource,
		FillDetailsStatus: resolved.fillStatus, FeeDetailsStatus: resolved.feeStatus, Checked: true,
	}
	if resolved.status != "" && resolved.status != trade.Status {
		update.Status = resolved.status
		update.LifecycleDetail = "Authoritative venue order and execution evidence was reconciled."
	}
	if resolved.status == intent.TradeUnknown {
		update.PublicError = "The venue outcome is not yet conclusive. Recheck later."
		update.LifecycleDetail = "Recheck did not find conclusive terminal venue evidence."
	}
	if resolved.definitive {
		update.ClearPublicError = true
	}
	if resolved.status.Terminal() {
		update.BalanceSyncStatus = "PENDING"
	}
	updated, err := s.Store.UpdateQuickTrade(ctx, intentID, update, now)
	if err != nil {
		return intent.QuickTrade{}, err
	}
	if resolved.status.Terminal() && s.Accounts != nil {
		balanceStatus := "SYNCED"
		if refreshErr := s.Accounts.Refresh(ctx, domain.Venue(trade.Quote.Venue)); refreshErr != nil {
			balanceStatus = "STALE"
		}
		updated, err = s.Store.UpdateQuickTrade(ctx, intentID, intent.QuickTradeUpdate{BalanceSyncStatus: balanceStatus, Checked: true}, now)
	}
	if err == nil && s.OnEvent != nil {
		s.OnEvent("trade.updated", domain.Venue(trade.Quote.Venue), intentID, now)
	}
	return updated, err
}

func (s *Service) Recover(ctx context.Context, now time.Time) error {
	trades, err := s.Store.ListQuickTrades(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, trade := range trades {
		if !trade.Status.Active() {
			continue
		}
		if _, err := s.RecheckTrade(ctx, trade.ID, now); err != nil {
			errs = append(errs, fmt.Errorf("recheck %s: %w", trade.ID, err))
		}
	}
	return errors.Join(errs...)
}

type fill struct {
	qty, price, fee *big.Rat
	feeAsset        string
}

func summarizeFills(side domain.Side, requested string, fills []fill) (resolution, error) {
	requestedQty, ok := positive(requested)
	if !ok {
		return resolution{}, errors.New("trade requested quantity is invalid")
	}
	filled, quoteTotal := new(big.Rat), new(big.Rat)
	fees := make(map[string]*big.Rat)
	for _, item := range fills {
		if item.qty == nil || item.price == nil || item.qty.Sign() <= 0 || item.price.Sign() <= 0 {
			return resolution{}, errors.New("venue execution contains invalid quantity or price")
		}
		filled.Add(filled, item.qty)
		quoteTotal.Add(quoteTotal, new(big.Rat).Mul(item.qty, item.price))
		if item.fee != nil && item.fee.Sign() != 0 {
			asset := strings.ToUpper(strings.TrimSpace(item.feeAsset))
			if asset == "" {
				return resolution{}, errors.New("venue execution fee asset is missing")
			}
			amount := new(big.Rat).Abs(item.fee)
			if fees[asset] == nil {
				fees[asset] = new(big.Rat)
			}
			fees[asset].Add(fees[asset], amount)
		}
	}
	result := resolution{filledBaseQty: decimal(filled), fillStatus: "COMPLETE", feeStatus: "COMPLETE"}
	if filled.Sign() == 0 {
		return result, nil
	}
	result.averagePrice = decimal(new(big.Rat).Quo(quoteTotal, filled))
	baseGross, quoteGross := filled, quoteTotal
	if side == domain.SideBuy {
		result.grossSource, result.grossDestination = decimal(quoteGross), decimal(baseGross)
	} else {
		result.grossSource, result.grossDestination = decimal(baseGross), decimal(quoteGross)
	}
	result.netDestination = result.grossDestination
	result.actualSource = result.grossSource
	assets := make([]string, 0, len(fees))
	for asset := range fees {
		assets = append(assets, asset)
	}
	sort.Strings(assets)
	for _, asset := range assets {
		amount := fees[asset]
		result.fees = append(result.fees, intent.TradeFee{Asset: asset, Amount: decimal(amount)})
	}
	if filled.Cmp(requestedQty) >= 0 {
		result.status = intent.TradeFilled
	}
	return result, nil
}

func applyAssets(result *resolution, side domain.Side, base, quote string) {
	source, destination := base, quote
	if side == domain.SideBuy {
		source, destination = quote, base
	}
	if result.netDestination == "" {
		result.netDestination = result.grossDestination
	}
	if result.actualSource == "" {
		result.actualSource = result.grossSource
	}
	for _, fee := range result.fees {
		amount, ok := nonNegative(fee.Amount)
		if !ok {
			continue
		}
		if fee.Asset == destination {
			value, ok := nonNegative(result.netDestination)
			if ok {
				value.Sub(value, amount)
				if value.Sign() < 0 {
					value.SetInt64(0)
				}
				result.netDestination = decimal(value)
			}
		}
		if fee.Asset == source {
			value, ok := nonNegative(result.actualSource)
			if ok {
				value.Add(value, amount)
				result.actualSource = decimal(value)
			}
		}
	}
}

func positive(raw string) (*big.Rat, bool) {
	value, ok := nonNegative(raw)
	return value, ok && value.Sign() > 0
}
func nonNegative(raw string) (*big.Rat, bool) {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	return value, ok && value.Sign() >= 0
}
func signedDecimal(raw string) (*big.Rat, bool) {
	return new(big.Rat).SetString(strings.TrimSpace(raw))
}
func newZero() *big.Rat { return new(big.Rat) }
func baseAndQuote(trade intent.QuickTrade) (string, string) {
	if domain.Side(trade.Quote.Side) == domain.SideBuy {
		return trade.Quote.ToAsset, trade.Quote.FromAsset
	}
	return trade.Quote.FromAsset, trade.Quote.ToAsset
}
func decimal(value *big.Rat) string {
	text := strings.TrimRight(strings.TrimRight(value.FloatString(18), "0"), ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}
