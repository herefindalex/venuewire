package quicktrade

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"venuewire/internal/domain"
	"venuewire/internal/intent"
)

type Route struct {
	ID         string
	Venue      domain.Venue
	Instrument string
	BaseAsset  string
	QuoteAsset string
	FromAsset  string
	ToAsset    string
	Side       domain.Side
}

var supportedRoutes = map[string]Route{
	"bybit-usdt-btc":   {ID: "bybit-usdt-btc", Venue: domain.VenueBybit, Instrument: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "USDT", FromAsset: "USDT", ToAsset: "BTC", Side: domain.SideBuy},
	"bybit-btc-usdt":   {ID: "bybit-btc-usdt", Venue: domain.VenueBybit, Instrument: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "USDT", FromAsset: "BTC", ToAsset: "USDT", Side: domain.SideSell},
	"bybit-usdt-eth":   {ID: "bybit-usdt-eth", Venue: domain.VenueBybit, Instrument: "ETHUSDT", BaseAsset: "ETH", QuoteAsset: "USDT", FromAsset: "USDT", ToAsset: "ETH", Side: domain.SideBuy},
	"bybit-eth-usdt":   {ID: "bybit-eth-usdt", Venue: domain.VenueBybit, Instrument: "ETHUSDT", BaseAsset: "ETH", QuoteAsset: "USDT", FromAsset: "ETH", ToAsset: "USDT", Side: domain.SideSell},
	"deribit-usdc-btc": {ID: "deribit-usdc-btc", Venue: domain.VenueDeribit, Instrument: "BTC_USDC", BaseAsset: "BTC", QuoteAsset: "USDC", FromAsset: "USDC", ToAsset: "BTC", Side: domain.SideBuy},
	"deribit-btc-usdc": {ID: "deribit-btc-usdc", Venue: domain.VenueDeribit, Instrument: "BTC_USDC", BaseAsset: "BTC", QuoteAsset: "USDC", FromAsset: "BTC", ToAsset: "USDC", Side: domain.SideSell},
}

type InstrumentRules struct {
	Instrument       string
	BaseAsset        string
	QuoteAsset       string
	TickSize         string
	QuantityStep     string
	MinimumQuantity  string
	MinimumNotional  string
	MaximumQuantity  string
	MetadataRevision string
}

type BookLevel struct {
	Price    string
	Quantity string
}

type MarketSnapshot struct {
	Rules      InstrumentRules
	Bids       []BookLevel
	Asks       []BookLevel
	ObservedAt time.Time
}

type Capacity struct {
	Available       string
	AccountRevision uint64
	ObservedAt      time.Time
}

type FeePolicy struct {
	Rate             string
	ChargeAsset      string
	ThirdAsset       string
	ThirdAssetAmount string
	Source           string
}

type Provider interface {
	Market(context.Context, Route) (MarketSnapshot, error)
	Available(context.Context, Route, string) (Capacity, error)
	Fee(context.Context, Route) (FeePolicy, error)
}

type Caps struct {
	ByAsset       map[string]string
	ByVenueSource map[string]string
}

type Service struct {
	Providers   map[domain.Venue]Provider
	TTL         time.Duration
	BookMaxAge  time.Duration
	SlippageBPS int
	Caps        Caps
	Now         func() time.Time
	NewID       func() (string, error)
}

type CreateRequest struct {
	Identity     string
	Venue        domain.Venue
	RouteID      string
	SpendBudget  string
	AccountAlias string
}

type Error struct {
	Code          string
	PublicMessage string
	Err           error
}

func (e *Error) Error() string {
	if e.PublicMessage != "" {
		return e.PublicMessage
	}
	return e.Err.Error()
}
func (e *Error) Unwrap() error { return e.Err }

func Routes() []Route {
	return []Route{
		supportedRoutes["bybit-usdt-btc"], supportedRoutes["bybit-btc-usdt"],
		supportedRoutes["bybit-usdt-eth"], supportedRoutes["bybit-eth-usdt"],
		supportedRoutes["deribit-usdc-btc"], supportedRoutes["deribit-btc-usdc"],
	}
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (intent.QuickTradeQuote, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if strings.TrimSpace(request.Identity) == "" || strings.TrimSpace(request.AccountAlias) == "" {
		return intent.QuickTradeQuote{}, quoteError("INVALID_REQUEST", "identity and account alias are required")
	}
	route, ok := supportedRoutes[request.RouteID]
	if !ok || route.Venue != request.Venue {
		return intent.QuickTradeQuote{}, quoteError("UNSUPPORTED_ROUTE", "the requested trade route is not supported")
	}
	provider := s.Providers[request.Venue]
	if provider == nil {
		return intent.QuickTradeQuote{}, quoteError("VENUE_UNAVAILABLE", "the selected venue is unavailable")
	}
	if s.TTL <= 0 || s.BookMaxAge <= 0 || s.SlippageBPS <= 0 || s.SlippageBPS > 50 {
		return intent.QuickTradeQuote{}, errors.New("quick trade service configuration is invalid")
	}
	spend, err := positiveDecimal("spend budget", request.SpendBudget)
	if err != nil {
		return intent.QuickTradeQuote{}, quoteError("INVALID_AMOUNT", err.Error())
	}
	if err := validateCap(s.Caps, route, spend); err != nil {
		return intent.QuickTradeQuote{}, err
	}

	market, err := provider.Market(ctx, route)
	if err != nil {
		return intent.QuickTradeQuote{}, providerError("MARKET_UNAVAILABLE", "Executable Spot market data is unavailable.", err)
	}
	rules, err := parseRules(route, market.Rules)
	if err != nil {
		return intent.QuickTradeQuote{}, err
	}
	book, err := parseBook(market, now, s.BookMaxAge)
	if err != nil {
		return intent.QuickTradeQuote{}, err
	}
	fee, err := provider.Fee(ctx, route)
	if err != nil {
		return blockedQuote(s, request, route, market, now, "FEE_MODEL_UNAVAILABLE", "Fee information could not be verified."), nil
	}
	feeRate, err := parseFeePolicy(route, fee)
	if err != nil {
		return blockedQuote(s, request, route, market, now, "FEE_MODEL_UNAVAILABLE", "Fee information could not be verified."), nil
	}

	limitPrice := protectedLimit(route.Side, book.bestBid, book.bestAsk, rules.tick, s.SlippageBPS)
	if limitPrice == nil || (route.Side == domain.SideBuy && limitPrice.Cmp(book.bestAsk) < 0) || (route.Side == domain.SideSell && limitPrice.Cmp(book.bestBid) > 0) {
		return intent.QuickTradeQuote{}, quoteError("PRICE_PROTECTION_UNAVAILABLE", "no marketable legal tick exists inside the price protection limit")
	}
	baseQuantity := maximumBaseQuantity(route.Side, spend, limitPrice, rules.step, fee.ChargeAsset, feeRate)
	if baseQuantity.Sign() <= 0 {
		return intent.QuickTradeQuote{}, quoteError("AMOUNT_TOO_SMALL", "the amount is below the legal quantity step")
	}
	if baseQuantity.Cmp(rules.minimumQuantity) < 0 {
		return intent.QuickTradeQuote{}, quoteError("AMOUNT_TOO_SMALL", "the amount is below the venue minimum quantity")
	}
	if rules.maximumQuantity != nil && baseQuantity.Cmp(rules.maximumQuantity) > 0 {
		return intent.QuickTradeQuote{}, quoteError("AMOUNT_TOO_LARGE", "the amount exceeds the venue maximum quantity")
	}
	orderNotional := new(big.Rat).Mul(baseQuantity, limitPrice)
	if rules.minimumNotional != nil && orderNotional.Cmp(rules.minimumNotional) < 0 {
		return intent.QuickTradeQuote{}, quoteError("AMOUNT_TOO_SMALL", "the amount is below the venue minimum notional")
	}

	sourceDebit := sourceDebitUpperBound(route.Side, baseQuantity, limitPrice, fee.ChargeAsset, feeRate)
	sourceCapacity, err := provider.Available(ctx, route, route.FromAsset)
	if err != nil {
		return intent.QuickTradeQuote{}, providerError("CAPACITY_UNAVAILABLE", "Available-to-trade capacity could not be verified.", err)
	}
	available, err := nonNegativeDecimal("available to trade", sourceCapacity.Available)
	if err != nil {
		return intent.QuickTradeQuote{}, quoteError("CAPACITY_UNAVAILABLE", "available-to-trade capacity is invalid")
	}
	if sourceDebit.Cmp(available) > 0 {
		return intent.QuickTradeQuote{}, quoteError("INSUFFICIENT_SPOT_BALANCE", "the source amount exceeds available Spot funds")
	}

	depthQuantity, grossDestination := executableEstimate(route.Side, baseQuantity, limitPrice, book)
	warnings := []string{"Limit IOC may fill partially or receive no fill."}
	if depthQuantity.Cmp(baseQuantity) < 0 {
		warnings = append(warnings, "Visible protected-price depth is smaller than the submitted quantity.")
	}
	estimatedFees, thirdReserves, netDestination, err := feeEstimate(ctx, provider, route, fee, feeRate, depthQuantity, grossDestination, limitPrice)
	if err != nil {
		return intent.QuickTradeQuote{}, err
	}

	id, err := s.id()
	if err != nil {
		return intent.QuickTradeQuote{}, err
	}
	return intent.QuickTradeQuote{
		ID: id, Identity: request.Identity, Venue: string(route.Venue), Environment: "testnet",
		AccountAlias: request.AccountAlias, RouteID: route.ID, FromAsset: route.FromAsset, ToAsset: route.ToAsset,
		SpendBudget: decimalString(spend), Instrument: route.Instrument, Side: string(route.Side),
		BaseQty: decimalString(baseQuantity), LimitPrice: decimalString(limitPrice), TimeInForce: "IOC",
		ReferenceBid: decimalString(book.bestBid), ReferenceAsk: decimalString(book.bestAsk),
		BookObservedAt: market.ObservedAt, MetadataRevision: market.Rules.MetadataRevision,
		PriceProtectionBPS: s.SlippageBPS, GrossReceiveEstimate: decimalString(grossDestination),
		NetReceiveEstimate: decimalString(netDestination), EstimatedFees: estimatedFees,
		FeeRate: decimalString(feeRate), FeeChargeAsset: fee.ChargeAsset,
		SourceDebitUpperBound: decimalString(sourceDebit), ThirdAssetReserves: thirdReserves,
		AccountRevision: sourceCapacity.AccountRevision, CreatedAt: now, ExpiresAt: now.Add(s.TTL),
		Warnings: warnings, Executable: true,
	}, nil
}

func (s *Service) ValidateForConfirm(ctx context.Context, quote intent.QuickTradeQuote) error {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if !quote.Executable {
		return quoteError("QUOTE_NOT_EXECUTABLE", "the quote is not executable")
	}
	if !now.Before(quote.ExpiresAt) {
		return quoteError("QUOTE_EXPIRED", "the quote expired")
	}
	route, ok := supportedRoutes[quote.RouteID]
	if !ok || string(route.Venue) != quote.Venue || route.Instrument != quote.Instrument || route.FromAsset != quote.FromAsset || route.ToAsset != quote.ToAsset || string(route.Side) != quote.Side {
		return quoteError("QUOTE_SCOPE_MISMATCH", "the quote route is invalid")
	}
	provider := s.Providers[route.Venue]
	if provider == nil {
		return quoteError("VENUE_UNAVAILABLE", "the selected venue is unavailable")
	}
	spend, err := positiveDecimal("spend budget", quote.SpendBudget)
	if err != nil {
		return quoteError("INVALID_QUOTE", "the quote amount is invalid")
	}
	if err := validateCap(s.Caps, route, spend); err != nil {
		return err
	}
	market, err := provider.Market(ctx, route)
	if err != nil {
		return providerError("MARKET_UNAVAILABLE", "Executable Spot market data is unavailable.", err)
	}
	rules, err := parseRules(route, market.Rules)
	if err != nil {
		return err
	}
	if market.Rules.MetadataRevision == "" || market.Rules.MetadataRevision != quote.MetadataRevision {
		return quoteError("QUOTE_CHANGED", "instrument rules changed; review a new quote")
	}
	book, err := parseBook(market, now, s.BookMaxAge)
	if err != nil {
		return err
	}
	limit, err := positiveDecimal("quote limit price", quote.LimitPrice)
	if err != nil || floorToStep(limit, rules.tick).Cmp(limit) != 0 {
		return quoteError("INVALID_QUOTE", "the quote limit price is not a legal tick")
	}
	quantity, err := positiveDecimal("quote base quantity", quote.BaseQty)
	if err != nil || floorToStep(quantity, rules.step).Cmp(quantity) != 0 || quantity.Cmp(rules.minimumQuantity) < 0 {
		return quoteError("INVALID_QUOTE", "the quote quantity is no longer legal")
	}
	if rules.maximumQuantity != nil && quantity.Cmp(rules.maximumQuantity) > 0 {
		return quoteError("INVALID_QUOTE", "the quote quantity exceeds current instrument rules")
	}
	if rules.minimumNotional != nil && new(big.Rat).Mul(quantity, limit).Cmp(rules.minimumNotional) < 0 {
		return quoteError("INVALID_QUOTE", "the quote no longer meets minimum notional")
	}
	if (route.Side == domain.SideBuy && book.bestAsk.Cmp(limit) > 0) || (route.Side == domain.SideSell && book.bestBid.Cmp(limit) < 0) {
		return quoteError("QUOTE_CHANGED", "the protected limit is no longer marketable; review a new quote")
	}
	debit, err := positiveDecimal("source debit upper bound", quote.SourceDebitUpperBound)
	if err != nil {
		return quoteError("INVALID_QUOTE", "the quote source debit is invalid")
	}
	capacity, err := provider.Available(ctx, route, route.FromAsset)
	if err != nil {
		return providerError("CAPACITY_UNAVAILABLE", "Available-to-trade capacity could not be verified.", err)
	}
	available, err := nonNegativeDecimal("available to trade", capacity.Available)
	if err != nil || debit.Cmp(available) > 0 {
		return quoteError("INSUFFICIENT_SPOT_BALANCE", "available Spot funds changed; review a new quote")
	}
	fee, err := provider.Fee(ctx, route)
	if err != nil {
		return quoteError("FEE_MODEL_UNAVAILABLE", "fee information could not be revalidated")
	}
	currentFeeRate, err := parseFeePolicy(route, fee)
	quotedFeeRate, quotedRateErr := nonNegativeDecimal("quoted fee rate", quote.FeeRate)
	if err != nil || quotedRateErr != nil || currentFeeRate.Cmp(quotedFeeRate) != 0 ||
		quote.FeeChargeAsset != fee.ChargeAsset || len(quote.EstimatedFees) != 1 || quote.EstimatedFees[0].Source != fee.Source {
		return quoteError("QUOTE_CHANGED", "fee information changed; review a new quote")
	}
	if fee.ChargeAsset == "third" {
		if len(quote.ThirdAssetReserves) != 1 || quote.ThirdAssetReserves[0].Asset != strings.ToUpper(fee.ThirdAsset) || quote.ThirdAssetReserves[0].Amount != fee.ThirdAssetAmount {
			return quoteError("QUOTE_CHANGED", "fee reserve changed; review a new quote")
		}
		thirdCapacity, err := provider.Available(ctx, route, fee.ThirdAsset)
		if err != nil {
			return providerError("CAPACITY_UNAVAILABLE", "Fee-asset capacity could not be verified.", err)
		}
		thirdAvailable, parseErr := nonNegativeDecimal("fee asset capacity", thirdCapacity.Available)
		thirdRequired, requiredErr := positiveDecimal("fee asset reserve", fee.ThirdAssetAmount)
		if parseErr != nil || requiredErr != nil || thirdAvailable.Cmp(thirdRequired) < 0 {
			return quoteError("INSUFFICIENT_FEE_ASSET", "the required fee asset is unavailable")
		}
	}
	return nil
}

type parsedRules struct {
	tick, step, minimumQuantity      *big.Rat
	minimumNotional, maximumQuantity *big.Rat
}

type parsedBook struct {
	bestBid, bestAsk *big.Rat
	bids, asks       []parsedLevel
}

type parsedLevel struct{ price, quantity *big.Rat }

func parseRules(route Route, rules InstrumentRules) (parsedRules, error) {
	if rules.Instrument != route.Instrument || strings.ToUpper(rules.BaseAsset) != route.BaseAsset || strings.ToUpper(rules.QuoteAsset) != route.QuoteAsset {
		return parsedRules{}, quoteError("METADATA_MISMATCH", "venue instrument metadata does not match the configured route")
	}
	tick, err := positiveDecimal("tick size", rules.TickSize)
	if err != nil {
		return parsedRules{}, quoteError("INVALID_INSTRUMENT_RULES", err.Error())
	}
	step, err := positiveDecimal("quantity step", rules.QuantityStep)
	if err != nil {
		return parsedRules{}, quoteError("INVALID_INSTRUMENT_RULES", err.Error())
	}
	minimumQuantity, err := positiveDecimal("minimum quantity", rules.MinimumQuantity)
	if err != nil {
		return parsedRules{}, quoteError("INVALID_INSTRUMENT_RULES", err.Error())
	}
	result := parsedRules{tick: tick, step: step, minimumQuantity: minimumQuantity}
	if rules.MinimumNotional != "" {
		result.minimumNotional, err = positiveDecimal("minimum notional", rules.MinimumNotional)
		if err != nil {
			return parsedRules{}, quoteError("INVALID_INSTRUMENT_RULES", err.Error())
		}
	}
	if rules.MaximumQuantity != "" {
		result.maximumQuantity, err = positiveDecimal("maximum quantity", rules.MaximumQuantity)
		if err != nil {
			return parsedRules{}, quoteError("INVALID_INSTRUMENT_RULES", err.Error())
		}
	}
	return result, nil
}

func parseBook(market MarketSnapshot, now time.Time, maxAge time.Duration) (parsedBook, error) {
	if market.ObservedAt.IsZero() || now.Sub(market.ObservedAt) > maxAge || market.ObservedAt.After(now.Add(time.Second)) {
		return parsedBook{}, quoteError("STALE_MARKET_DATA", "market data is stale")
	}
	bids, err := parseLevels("bid", market.Bids)
	if err != nil {
		return parsedBook{}, err
	}
	asks, err := parseLevels("ask", market.Asks)
	if err != nil {
		return parsedBook{}, err
	}
	if len(bids) == 0 || len(asks) == 0 || bids[0].price.Cmp(asks[0].price) >= 0 {
		return parsedBook{}, quoteError("INVALID_BOOK", "market book is empty or crossed")
	}
	for index := 1; index < len(bids); index++ {
		if bids[index-1].price.Cmp(bids[index].price) < 0 {
			return parsedBook{}, quoteError("INVALID_BOOK", "bid levels are not in executable order")
		}
	}
	for index := 1; index < len(asks); index++ {
		if asks[index-1].price.Cmp(asks[index].price) > 0 {
			return parsedBook{}, quoteError("INVALID_BOOK", "ask levels are not in executable order")
		}
	}
	return parsedBook{bestBid: bids[0].price, bestAsk: asks[0].price, bids: bids, asks: asks}, nil
}

func parseLevels(side string, levels []BookLevel) ([]parsedLevel, error) {
	result := make([]parsedLevel, 0, len(levels))
	for _, level := range levels {
		price, err := positiveDecimal(side+" price", level.Price)
		if err != nil {
			return nil, quoteError("INVALID_BOOK", err.Error())
		}
		quantity, err := positiveDecimal(side+" quantity", level.Quantity)
		if err != nil {
			return nil, quoteError("INVALID_BOOK", err.Error())
		}
		result = append(result, parsedLevel{price: price, quantity: quantity})
	}
	return result, nil
}

func protectedLimit(side domain.Side, bid, ask, tick *big.Rat, bps int) *big.Rat {
	if side == domain.SideBuy {
		protected := new(big.Rat).Mul(ask, big.NewRat(int64(10_000+bps), 10_000))
		return floorToStep(protected, tick)
	}
	protected := new(big.Rat).Mul(bid, big.NewRat(int64(10_000-bps), 10_000))
	return ceilToStep(protected, tick)
}

func maximumBaseQuantity(side domain.Side, spend, limit, step *big.Rat, chargeAsset string, feeRate *big.Rat) *big.Rat {
	denominator := new(big.Rat).Set(limit)
	if side == domain.SideSell {
		denominator.SetInt64(1)
	}
	if chargeAsset == "from" {
		denominator.Mul(denominator, new(big.Rat).Add(big.NewRat(1, 1), feeRate))
	}
	return floorToStep(new(big.Rat).Quo(spend, denominator), step)
}

func sourceDebitUpperBound(side domain.Side, quantity, limit *big.Rat, chargeAsset string, feeRate *big.Rat) *big.Rat {
	debit := new(big.Rat).Set(quantity)
	if side == domain.SideBuy {
		debit.Mul(debit, limit)
	}
	if chargeAsset == "from" {
		debit.Mul(debit, new(big.Rat).Add(big.NewRat(1, 1), feeRate))
	}
	return debit
}

func executableEstimate(side domain.Side, requested, limit *big.Rat, book parsedBook) (*big.Rat, *big.Rat) {
	levels := book.asks
	if side == domain.SideSell {
		levels = book.bids
	}
	remaining := new(big.Rat).Set(requested)
	filled := new(big.Rat)
	received := new(big.Rat)
	for _, level := range levels {
		if (side == domain.SideBuy && level.price.Cmp(limit) > 0) || (side == domain.SideSell && level.price.Cmp(limit) < 0) {
			break
		}
		take := minRat(remaining, level.quantity)
		filled.Add(filled, take)
		if side == domain.SideSell {
			received.Add(received, new(big.Rat).Mul(take, level.price))
		}
		remaining.Sub(remaining, take)
		if remaining.Sign() == 0 {
			break
		}
	}
	if side == domain.SideBuy {
		received.Set(filled)
	}
	return filled, received
}

func feeEstimate(ctx context.Context, provider Provider, route Route, policy FeePolicy, rate, filled, grossDestination, limitPrice *big.Rat) ([]intent.QuoteFee, []intent.AssetReserve, *big.Rat, error) {
	net := new(big.Rat).Set(grossDestination)
	fees := []intent.QuoteFee{}
	reserves := []intent.AssetReserve{}
	feeAmount := new(big.Rat)
	asset := route.ToAsset
	basis := "destination received"
	switch policy.ChargeAsset {
	case "from":
		asset, basis = route.FromAsset, "source debit upper bound"
		feeBase := new(big.Rat).Set(filled)
		if route.Side == domain.SideBuy {
			feeBase.Mul(feeBase, limitPrice)
		}
		feeAmount.Mul(feeBase, rate)
	case "to":
		feeAmount.Mul(grossDestination, rate)
		net.Sub(net, feeAmount)
	case "third":
		third, err := positiveDecimal("third asset fee reserve", policy.ThirdAssetAmount)
		if err != nil || strings.TrimSpace(policy.ThirdAsset) == "" {
			return nil, nil, nil, quoteError("FEE_MODEL_UNAVAILABLE", "third-asset fee reserve is unavailable")
		}
		capacity, err := provider.Available(ctx, route, policy.ThirdAsset)
		if err != nil {
			return nil, nil, nil, providerError("CAPACITY_UNAVAILABLE", "Fee-asset capacity could not be verified.", err)
		}
		available, err := nonNegativeDecimal("third asset available", capacity.Available)
		if err != nil || available.Cmp(third) < 0 {
			return nil, nil, nil, quoteError("INSUFFICIENT_FEE_ASSET", "the required fee asset is unavailable")
		}
		asset, basis, feeAmount = strings.ToUpper(policy.ThirdAsset), "third-asset reserve", third
		reserves = append(reserves, intent.AssetReserve{Asset: asset, Amount: decimalString(third)})
	default:
		return nil, nil, nil, quoteError("FEE_MODEL_UNAVAILABLE", "fee charge asset is unknown")
	}
	fees = append(fees, intent.QuoteFee{Asset: asset, EstimatedAmount: decimalString(feeAmount), Source: policy.Source, CalculationBasis: basis})
	return fees, reserves, net, nil
}

func parseFeePolicy(route Route, policy FeePolicy) (*big.Rat, error) {
	if policy.Source == "" || (policy.ChargeAsset != "from" && policy.ChargeAsset != "to" && policy.ChargeAsset != "third") {
		return nil, errors.New("fee policy lacks source or charge asset")
	}
	if policy.ChargeAsset == "third" {
		return new(big.Rat), nil
	}
	rate, err := nonNegativeDecimal("fee rate", policy.Rate)
	if err != nil {
		return nil, err
	}
	if rate.Cmp(big.NewRat(1, 10)) > 0 {
		return nil, errors.New("fee rate exceeds supported safety bound")
	}
	_ = route
	return rate, nil
}

func validateCap(caps Caps, route Route, spend *big.Rat) error {
	values := []string{caps.ByAsset[route.FromAsset], caps.ByVenueSource[string(route.Venue)+":"+route.FromAsset]}
	for _, raw := range values {
		capValue, err := positiveDecimal("quick trade cap", raw)
		if err != nil {
			return errors.New("quick trade cap configuration is invalid")
		}
		if spend.Cmp(capValue) > 0 {
			return quoteError("DEMO_AMOUNT_LIMIT", "the amount exceeds the configured demo limit")
		}
	}
	return nil
}

func blockedQuote(s *Service, request CreateRequest, route Route, market MarketSnapshot, now time.Time, reason, warning string) intent.QuickTradeQuote {
	id, _ := s.id()
	return intent.QuickTradeQuote{ID: id, Identity: request.Identity, Venue: string(route.Venue), Environment: "testnet", AccountAlias: request.AccountAlias, RouteID: route.ID, FromAsset: route.FromAsset, ToAsset: route.ToAsset, SpendBudget: request.SpendBudget, Instrument: route.Instrument, Side: string(route.Side), TimeInForce: "IOC", BookObservedAt: market.ObservedAt, MetadataRevision: market.Rules.MetadataRevision, PriceProtectionBPS: s.SlippageBPS, CreatedAt: now, ExpiresAt: now.Add(s.TTL), Warnings: []string{warning}, Executable: false, BlockedReason: reason}
}

func (s *Service) id() (string, error) {
	if s.NewID != nil {
		return s.NewID()
	}
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "quote_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)), nil
}

func positiveDecimal(name, raw string) (*big.Rat, error) {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	if !ok || value.Sign() <= 0 {
		return nil, fmt.Errorf("%s must be a positive decimal", name)
	}
	return value, nil
}

func nonNegativeDecimal(name, raw string) (*big.Rat, error) {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	if !ok || value.Sign() < 0 {
		return nil, fmt.Errorf("%s must be a non-negative decimal", name)
	}
	return value, nil
}

func floorToStep(value, step *big.Rat) *big.Rat {
	units := new(big.Rat).Quo(value, step)
	whole := new(big.Int).Quo(units.Num(), units.Denom())
	return new(big.Rat).Mul(new(big.Rat).SetInt(whole), step)
}

func ceilToStep(value, step *big.Rat) *big.Rat {
	units := new(big.Rat).Quo(value, step)
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(units.Num(), units.Denom(), remainder)
	if remainder.Sign() != 0 {
		whole.Add(whole, big.NewInt(1))
	}
	return new(big.Rat).Mul(new(big.Rat).SetInt(whole), step)
}

func minRat(left, right *big.Rat) *big.Rat {
	if left.Cmp(right) <= 0 {
		return new(big.Rat).Set(left)
	}
	return new(big.Rat).Set(right)
}

func decimalString(value *big.Rat) string {
	text := value.FloatString(18)
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}

func quoteError(code, message string) error {
	return &Error{Code: code, Err: errors.New(message)}
}

func providerError(code, message string, cause error) error {
	return &Error{Code: code, PublicMessage: message, Err: cause}
}
