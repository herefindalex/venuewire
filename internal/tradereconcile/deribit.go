package tradereconcile

import (
	"context"
	"errors"
	"strings"

	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/intent"
)

func (s *Service) recheckDeribit(ctx context.Context, trade intent.QuickTrade) (resolution, error) {
	if s.Deribit == nil {
		return resolution{}, errors.New("Deribit reconciliation client is unavailable")
	}
	var orders []deribit.Order
	if trade.VenueOrderID != "" {
		order, err := s.Deribit.OrderState(ctx, trade.VenueOrderID)
		if err != nil {
			return resolution{}, err
		}
		orders = []deribit.Order{order}
	} else {
		matched, err := s.Deribit.OrdersByLabel(ctx, trade.Quote.Instrument, trade.ClientOrderID)
		if err != nil {
			return resolution{}, err
		}
		orders = matched
	}
	if len(orders) > 1 {
		return resolution{}, errors.New("multiple Deribit orders match one VenueWire intent")
	}
	if len(orders) == 0 {
		return resolution{status: intent.TradeUnknown, resultStatus: "OUTCOME_UNKNOWN", fillStatus: "INCOMPLETE", feeStatus: "INCOMPLETE"}, nil
	}
	order := orders[0]
	if order.OrderID == "" || order.InstrumentName != trade.Quote.Instrument || (order.Label != "" && order.Label != trade.ClientOrderID) {
		return resolution{}, errors.New("Deribit order evidence does not match the VenueWire intent")
	}
	trades, err := s.Deribit.TradesByOrder(ctx, order.OrderID)
	if err != nil {
		return resolution{}, err
	}
	fills := make([]fill, 0, len(trades))
	for _, execution := range trades {
		qty, qtyOK := positive(execution.Amount.String())
		price, priceOK := positive(execution.Price.String())
		if !qtyOK || !priceOK {
			return resolution{}, errors.New("Deribit trade contains invalid decimals")
		}
		fee, feeOK := signedDecimal(execution.Fee.String())
		if !feeOK && strings.TrimSpace(execution.Fee.String()) != "" {
			return resolution{}, errors.New("Deribit trade fee is invalid")
		}
		if !feeOK {
			fee = newZero()
		}
		fills = append(fills, fill{qty: qty, price: price, fee: fee, feeAsset: execution.FeeCurrency})
	}
	result, err := summarizeFills(domain.Side(trade.Quote.Side), trade.Quote.BaseQty, fills)
	if err != nil {
		return resolution{}, err
	}
	base, quote := baseAndQuote(trade)
	applyAssets(&result, domain.Side(trade.Quote.Side), base, quote)
	result.venueOrderID, result.rawStatus = order.OrderID, order.OrderState
	applyDeribitOrderStatus(&result, order.OrderState)
	return result, nil
}

func applyDeribitOrderStatus(result *resolution, raw string) {
	switch strings.ToLower(raw) {
	case "filled":
		detailsComplete := result.status == intent.TradeFilled
		result.status, result.resultStatus, result.definitive = intent.TradeFilled, "FILLED", true
		if !detailsComplete {
			result.fillStatus, result.feeStatus = "INCOMPLETE", "INCOMPLETE"
		}
	case "cancelled":
		result.status, result.resultStatus, result.definitive = intent.TradeCancelled, "CANCELLED_NO_FILL", true
		if result.filledBaseQty != "" && result.filledBaseQty != "0" {
			result.resultStatus = "PARTIALLY_FILLED_CANCELLED"
		}
	case "rejected":
		result.status, result.resultStatus, result.definitive = intent.TradeRejected, "REJECTED", true
	case "open", "untriggered":
		if result.filledBaseQty != "" && result.filledBaseQty != "0" {
			result.status, result.resultStatus = intent.TradePartiallyFilled, "PARTIALLY_FILLED"
		} else {
			result.status, result.resultStatus = intent.TradeAccepted, "ACCEPTED"
		}
	default:
		result.status, result.resultStatus = intent.TradeUnknown, "OUTCOME_UNKNOWN"
	}
}
