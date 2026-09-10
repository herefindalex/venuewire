package tradereconcile

import (
	"context"
	"errors"
	"strings"

	"venuewire/internal/domain"
	"venuewire/internal/intent"
)

func (s *Service) recheckBybit(ctx context.Context, trade intent.QuickTrade) (resolution, error) {
	if s.Bybit == nil {
		return resolution{}, errors.New("Bybit reconciliation client is unavailable")
	}
	orders, _, err := s.Bybit.Orders(ctx, "spot", trade.Quote.Instrument, trade.VenueOrderID, trade.ClientOrderID, nil)
	if err != nil {
		return resolution{}, err
	}
	executions, _, err := s.Bybit.Executions(ctx, "spot", trade.Quote.Instrument, trade.VenueOrderID, trade.ClientOrderID)
	if err != nil {
		return resolution{}, err
	}
	var matchedOrders []int
	for index, order := range orders {
		if (trade.VenueOrderID != "" && order.OrderID == trade.VenueOrderID) || order.OrderLinkID == trade.ClientOrderID {
			matchedOrders = append(matchedOrders, index)
		}
	}
	if len(matchedOrders) > 1 {
		return resolution{}, errors.New("multiple Bybit orders match one VenueWire intent")
	}
	fills := make([]fill, 0, len(executions))
	for _, execution := range executions {
		if execution.OrderLinkID != trade.ClientOrderID && (trade.VenueOrderID == "" || execution.OrderID != trade.VenueOrderID) {
			continue
		}
		qty, qtyOK := positive(execution.ExecQty)
		price, priceOK := positive(execution.ExecPrice)
		if !qtyOK || !priceOK {
			return resolution{}, errors.New("Bybit execution contains invalid decimals")
		}
		var feeValue = newZero()
		if strings.TrimSpace(execution.ExecFee) != "" {
			parsed, ok := signedDecimal(execution.ExecFee)
			if !ok {
				return resolution{}, errors.New("Bybit execution fee is invalid")
			}
			feeValue = parsed
		}
		fills = append(fills, fill{qty: qty, price: price, fee: feeValue, feeAsset: execution.FeeCurrency})
	}
	result, err := summarizeFills(domain.Side(trade.Quote.Side), trade.Quote.BaseQty, fills)
	if err != nil {
		return resolution{}, err
	}
	base, quote := baseAndQuote(trade)
	applyAssets(&result, domain.Side(trade.Quote.Side), base, quote)
	if len(matchedOrders) == 0 {
		result.status = intent.TradeUnknown
		result.resultStatus = "OUTCOME_UNKNOWN"
		result.fillStatus, result.feeStatus = "INCOMPLETE", "INCOMPLETE"
		return result, nil
	}
	order := orders[matchedOrders[0]]
	result.venueOrderID, result.rawStatus = order.OrderID, order.OrderStatus
	applyBybitOrderStatus(&result, order.OrderStatus)
	return result, nil
}

func applyBybitOrderStatus(result *resolution, raw string) {
	switch strings.ToLower(raw) {
	case "filled":
		detailsComplete := result.status == intent.TradeFilled
		result.status, result.resultStatus, result.definitive = intent.TradeFilled, "FILLED", true
		if !detailsComplete {
			result.fillStatus, result.feeStatus = "INCOMPLETE", "INCOMPLETE"
		}
	case "cancelled", "deactivated":
		result.status, result.resultStatus, result.definitive = intent.TradeCancelled, "CANCELLED_NO_FILL", true
		if result.filledBaseQty != "" && result.filledBaseQty != "0" {
			result.resultStatus = "PARTIALLY_FILLED_CANCELLED"
		}
	case "partiallyfilledcanceled", "partiallyfilledcancelled":
		result.status, result.resultStatus, result.definitive = intent.TradeCancelled, "PARTIALLY_FILLED_CANCELLED", true
	case "rejected":
		result.status, result.resultStatus, result.definitive = intent.TradeRejected, "REJECTED", true
	case "partiallyfilled":
		result.status, result.resultStatus = intent.TradePartiallyFilled, "PARTIALLY_FILLED"
	case "new", "created", "untriggered":
		result.status, result.resultStatus = intent.TradeAccepted, "ACCEPTED"
	default:
		result.status, result.resultStatus = intent.TradeUnknown, "OUTCOME_UNKNOWN"
	}
}
