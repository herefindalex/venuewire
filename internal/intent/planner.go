package intent

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"venuewire/internal/deribit"
)

type Market interface {
	Instrument(context.Context, string) (deribit.Instrument, error)
	Ticker(context.Context, string) (deribit.Ticker, error)
	OpenFutureOrdersByCurrency(context.Context, string) ([]deribit.Order, error)
	Place(context.Context, string, deribit.PlaceParams) (deribit.OrderResult, error)
	PlaceWS(context.Context, string, string, deribit.PlaceParams) (deribit.OrderResult, error)
	OrderState(context.Context, string) (deribit.Order, error)
}

type Limits struct {
	MaxOrderUSD, MaxAggregateOpenUSD, MaxPriceDeviationPct string
	MaxOpenOrders                                          int
	TTL                                                    time.Duration
}
type Request struct {
	Instrument, Side, OrderType, Amount, Price, TimeInForce string
	PostOnly, ReduceOnly                                    bool
	Transport                                               string
}
type Planner struct {
	Market         Market
	Store          Store
	Limits         Limits
	AccountAlias   string
	Now            func() time.Time
	NewID          func() (string, error)
	RiskCurrencies []string
	WSURL          string
	FIXPlace       func(context.Context, Plan) (deribit.OrderResult, error)
}
type ExecutionResult struct {
	Plan            Plan                `json:"plan"`
	Acknowledgement deribit.OrderResult `json:"acknowledgement"`
	ReadState       deribit.Order       `json:"readState"`
	Verified        bool                `json:"verified"`
}

func (p *Planner) ValidateRequest(ctx context.Context, request Request) (Plan, error) {
	return p.validate(ctx, request)
}

func (p *Planner) Create(ctx context.Context, request Request) (Plan, error) {
	plan, err := p.validate(ctx, request)
	if err != nil {
		return Plan{}, err
	}
	id, err := p.id()
	if err != nil {
		return Plan{}, err
	}
	now := p.now().UTC()
	plan.ID = id
	plan.CreatedAt = now
	plan.ExpiresAt = now.Add(p.Limits.TTL)
	plan.Status = StatusPlanned
	if err := p.Store.SavePlan(ctx, plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func (p *Planner) Execute(ctx context.Context, id string, confirm bool) (ExecutionResult, error) {
	if !confirm {
		return ExecutionResult{}, errors.New("execution requires --confirm")
	}
	stored, err := p.Store.Get(ctx, id)
	if err != nil {
		return ExecutionResult{}, err
	}
	if stored.Status != StatusPlanned {
		return ExecutionResult{}, fmt.Errorf("plan is not executable: %s", stored.Status)
	}
	if !p.now().Before(stored.ExpiresAt) {
		_, _ = p.Store.Claim(ctx, id, p.now())
		return ExecutionResult{}, errors.New("plan expired")
	}
	validated, err := p.validate(ctx, Request{Instrument: stored.Instrument, Side: stored.Side, OrderType: stored.OrderType, Amount: stored.Amount, Price: stored.Price, TimeInForce: stored.TimeInForce, PostOnly: stored.PostOnly, ReduceOnly: stored.ReduceOnly, Transport: stored.Transport})
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("plan revalidation failed: %w", err)
	}
	if validated.SettlementCurrency != stored.SettlementCurrency {
		return ExecutionResult{}, errors.New("instrument settlement metadata changed")
	}
	if stored.Transport == "fix" && p.FIXPlace == nil {
		return ExecutionResult{}, errors.New("Deribit FIX order executor is not configured")
	}
	claimed, err := p.Store.Claim(ctx, id, p.now())
	if err != nil {
		return ExecutionResult{}, err
	}
	params := deribit.PlaceParams{InstrumentName: claimed.Instrument, Amount: json.Number(claimed.Amount), Type: claimed.OrderType, Label: claimed.ID, Price: json.Number(claimed.Price), TimeInForce: claimed.TimeInForce, PostOnly: claimed.PostOnly, ReduceOnly: claimed.ReduceOnly}
	var ack deribit.OrderResult
	switch claimed.Transport {
	case "ws":
		ack, err = p.Market.PlaceWS(ctx, p.WSURL, claimed.Side, params)
	case "fix":
		ack, err = p.FIXPlace(ctx, claimed)
	default:
		ack, err = p.Market.Place(ctx, claimed.Side, params)
	}
	result := ExecutionResult{Plan: claimed, Acknowledgement: ack}
	if err != nil {
		var unknown *deribit.OutcomeUnknownError
		status, classification := StatusRejected, "exchange rejected order"
		if errors.As(err, &unknown) {
			status, classification = StatusOutcomeUnknown, "submission outcome unknown"
		}
		_ = p.Store.Finish(ctx, id, status, "", classification)
		result.Plan.Status = status
		return result, err
	}
	if ack.Order.OrderID == "" {
		_ = p.Store.Finish(ctx, id, StatusOutcomeUnknown, "", "acknowledgement missing order ID")
		result.Plan.Status = StatusOutcomeUnknown
		return result, errors.New("order acknowledgement missing native order ID")
	}
	state, readErr := p.Market.OrderState(ctx, ack.Order.OrderID)
	result.ReadState = state
	if readErr != nil || state.OrderID != ack.Order.OrderID || state.Label != id {
		_ = p.Store.Finish(ctx, id, StatusNeedsReview, ack.Order.OrderID, "independent order-state verification failed")
		result.Plan.Status = StatusNeedsReview
		if readErr != nil {
			return result, fmt.Errorf("independent order-state verification: %w", readErr)
		}
		return result, errors.New("independent order-state verification mismatch")
	}
	_ = p.Store.Finish(ctx, id, StatusSubmitted, ack.Order.OrderID, "")
	result.Plan.Status = StatusSubmitted
	result.Plan.NativeOrderID = ack.Order.OrderID
	result.Verified = true
	return result, nil
}

func (p *Planner) validate(ctx context.Context, request Request) (Plan, error) {
	if p.Market == nil {
		return Plan{}, errors.New("market client is required")
	}
	if p.Limits.MaxOpenOrders <= 0 || p.Limits.TTL <= 0 {
		return Plan{}, errors.New("risk limits and TTL must be positive")
	}
	instrument, err := p.Market.Instrument(ctx, request.Instrument)
	metadataAt := p.now().UTC()
	if err != nil {
		return Plan{}, err
	}
	if !instrument.IsActive || instrument.Kind != "future" || instrument.InstrumentName != request.Instrument {
		return Plan{}, errors.New("only an exact active Deribit future instrument is supported")
	}
	if instrument.InstrumentName != "BTC-PERPETUAL" && instrument.InstrumentName != "ETH-PERPETUAL" {
		return Plan{}, errors.New("only BTC-PERPETUAL and ETH-PERPETUAL are supported for trading")
	}
	if instrument.SettlementCurrency != "BTC" && instrument.SettlementCurrency != "ETH" {
		return Plan{}, errors.New("only native BTC/ETH collateral futures are supported")
	}
	amount, err := positiveRat("amount", request.Amount)
	if err != nil {
		return Plan{}, err
	}
	minimum, err := positiveRat("minimum amount", instrument.MinTradeAmount.String())
	if err != nil {
		return Plan{}, err
	}
	if amount.Cmp(minimum) < 0 || !isInteger(new(big.Rat).Quo(amount, minimum)) {
		return Plan{}, fmt.Errorf("amount must be a multiple of metadata minimum %s", instrument.MinTradeAmount.String())
	}
	maxOrder, err := positiveRat("max order USD", p.Limits.MaxOrderUSD)
	if err != nil {
		return Plan{}, err
	}
	if amount.Cmp(maxOrder) > 0 {
		return Plan{}, errors.New("order exceeds per-order USD risk limit")
	}
	side := strings.ToLower(request.Side)
	if side != "buy" && side != "sell" {
		return Plan{}, errors.New("side must be buy or sell")
	}
	orderType := strings.ToLower(request.OrderType)
	if orderType == "" {
		orderType = "limit"
	}
	if orderType != "limit" && orderType != "market" {
		return Plan{}, errors.New("type must be limit or market")
	}
	transport := strings.ToLower(request.Transport)
	if transport == "" {
		transport = "http"
	}
	if transport != "http" && transport != "ws" && transport != "fix" {
		return Plan{}, errors.New("transport must be http, ws, or fix")
	}
	ticker, err := p.Market.Ticker(ctx, request.Instrument)
	priceAt := p.now().UTC()
	if err != nil {
		return Plan{}, err
	}
	mark, err := positiveRat("mark price", ticker.MarkPrice.String())
	if err != nil {
		return Plan{}, err
	}
	priceText := request.Price
	validTick := instrument.TickSize.String()
	if orderType == "limit" {
		price, err := positiveRat("price", request.Price)
		if err != nil {
			return Plan{}, err
		}
		effectiveTick, err := instrument.EffectiveTickSize(request.Price)
		if err != nil {
			return Plan{}, err
		}
		validTick = effectiveTick.String()
		tick, err := positiveRat("tick size", validTick)
		if err != nil {
			return Plan{}, err
		}
		if !isInteger(new(big.Rat).Quo(price, tick)) {
			return Plan{}, fmt.Errorf("price must be a multiple of tick size %s", validTick)
		}
		deviation := new(big.Rat).Sub(price, mark)
		if deviation.Sign() < 0 {
			deviation.Neg(deviation)
		}
		deviation.Quo(deviation, mark)
		deviation.Mul(deviation, big.NewRat(100, 1))
		maxDeviation, err := positiveRat("max price deviation", p.Limits.MaxPriceDeviationPct)
		if err != nil {
			return Plan{}, err
		}
		if deviation.Cmp(maxDeviation) > 0 {
			return Plan{}, errors.New("limit price exceeds configured mark-price deviation")
		}
	} else {
		priceText = ""
	}
	currencies := p.RiskCurrencies
	if len(currencies) == 0 {
		currencies = []string{"BTC", "ETH"}
	}
	openCount := 0
	aggregate := new(big.Rat)
	for _, currency := range currencies {
		orders, err := p.Market.OpenFutureOrdersByCurrency(ctx, currency)
		if err != nil {
			return Plan{}, fmt.Errorf("read open %s orders: %w", currency, err)
		}
		openCount += len(orders)
		for _, order := range orders {
			value, err := positiveOrZeroRat("open order amount", order.Amount.String())
			if err != nil {
				return Plan{}, err
			}
			aggregate.Add(aggregate, value)
		}
	}
	if openCount >= p.Limits.MaxOpenOrders {
		return Plan{}, errors.New("maximum open-order count reached")
	}
	aggregate.Add(aggregate, amount)
	maxAggregate, err := positiveRat("max aggregate open USD", p.Limits.MaxAggregateOpenUSD)
	if err != nil {
		return Plan{}, err
	}
	if aggregate.Cmp(maxAggregate) > 0 {
		return Plan{}, errors.New("aggregate open USD risk limit exceeded")
	}
	return Plan{Venue: "deribit", Environment: "testnet", AccountAlias: p.AccountAlias, Instrument: request.Instrument, SettlementCurrency: instrument.SettlementCurrency, Side: side, OrderType: orderType, Transport: transport, Amount: request.Amount, AmountUnit: "USD_notional", Price: priceText, TimeInForce: request.TimeInForce, PostOnly: request.PostOnly, ReduceOnly: request.ReduceOnly, MarkPrice: ticker.MarkPrice.String(), ValidTick: validTick, EstimatedNotionalUSD: request.Amount, MetadataAt: metadataAt, PriceAt: priceAt, CODProtected: false, OpenOrderCount: openCount, AggregateOpenUSD: aggregate.FloatString(8)}, nil
}

func (p *Planner) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}
func (p *Planner) id() (string, error) {
	if p.NewID != nil {
		return p.NewID()
	}
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	stamp := p.now().UTC().UnixMilli()
	return fmt.Sprintf("di-%013d-%s", stamp, strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))[:8]), nil
}
func positiveRat(name, value string) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(value)
	if !ok || r.Sign() <= 0 {
		return nil, fmt.Errorf("%s must be a positive decimal", name)
	}
	return r, nil
}
func positiveOrZeroRat(name, value string) (*big.Rat, error) {
	if value == "" {
		value = "0"
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok || r.Sign() < 0 {
		return nil, fmt.Errorf("%s must be a non-negative decimal", name)
	}
	return r, nil
}
func isInteger(value *big.Rat) bool { return value.Denom().Cmp(big.NewInt(1)) == 0 }
