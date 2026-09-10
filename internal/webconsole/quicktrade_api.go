package webconsole

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"venuewire/internal/domain"
	"venuewire/internal/intent"
	"venuewire/internal/quicktrade"
)

type TradeApplication interface {
	CreateQuote(context.Context, quicktrade.CreateRequest) (intent.QuickTradeQuote, error)
	Confirm(context.Context, quicktrade.ConfirmRequest) (quicktrade.ConfirmResult, error)
}

type publicQuote struct {
	ID                    string                `json:"quoteId"`
	Venue                 string                `json:"venue"`
	Environment           string                `json:"environment"`
	AccountAlias          string                `json:"accountAlias"`
	RouteID               string                `json:"routeId"`
	FromAsset             string                `json:"fromAsset"`
	ToAsset               string                `json:"toAsset"`
	SpendBudget           string                `json:"spendBudget"`
	Instrument            string                `json:"instrument"`
	Side                  string                `json:"side"`
	BaseQty               string                `json:"baseQty"`
	LimitPrice            string                `json:"limitPrice"`
	TimeInForce           string                `json:"timeInForce"`
	ReferenceBid          string                `json:"referenceBid"`
	ReferenceAsk          string                `json:"referenceAsk"`
	BookObservedAt        string                `json:"bookObservedAt"`
	MetadataRevision      string                `json:"metadataRevision"`
	PriceProtectionBPS    int                   `json:"priceProtectionBps"`
	GrossReceiveEstimate  string                `json:"grossReceiveEstimate"`
	NetReceiveEstimate    string                `json:"netReceiveEstimate,omitempty"`
	EstimatedFees         []intent.QuoteFee     `json:"fees,omitempty"`
	SourceDebitUpperBound string                `json:"sourceDebitUpperBound"`
	ThirdAssetReserves    []intent.AssetReserve `json:"thirdAssetReserves,omitempty"`
	AccountRevision       uint64                `json:"accountRevision"`
	CreatedAt             string                `json:"createdAt"`
	ExpiresAt             string                `json:"expiresAt"`
	Warnings              []string              `json:"warnings,omitempty"`
	Executable            bool                  `json:"executable"`
	BlockedReason         string                `json:"blockedReason,omitempty"`
}

func (s *Server) handleCreateQuote(w http.ResponseWriter, r *http.Request) {
	if s.tradeApp == nil {
		writeError(w, http.StatusServiceUnavailable, "quote_unavailable", "Quick Trade is temporarily unavailable.", requestID(r))
		return
	}
	venue := domain.Venue(strings.ToLower(strings.TrimSpace(r.PathValue("venue"))))
	if (venue != domain.VenueBybit && venue != domain.VenueDeribit) || (venue == domain.VenueBybit && !s.config.BybitEnabled) || (venue == domain.VenueDeribit && !s.config.DeribitEnabled) {
		writeError(w, http.StatusNotFound, "venue_not_found", "The venue is not available.", requestID(r))
		return
	}
	var input struct {
		RouteID string `json:"routeId"`
		Amount  string `json:"amount"`
	}
	if err := decodeBrowserJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "The quote request is invalid.", requestID(r))
		return
	}
	accountAlias := s.base.AccountAlias
	if venue == domain.VenueDeribit {
		accountAlias = s.base.Deribit.AccountAlias
	}
	quote, err := s.tradeApp.CreateQuote(r.Context(), quicktrade.CreateRequest{
		Identity: s.config.Username, Venue: venue, RouteID: input.RouteID,
		SpendBudget: input.Amount, AccountAlias: accountAlias,
	})
	if err != nil {
		s.writeTradeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"quote": toPublicQuote(quote)})
}

func (s *Server) handleConfirmTrade(w http.ResponseWriter, r *http.Request) {
	if !s.config.TradingEnabled {
		writeError(w, http.StatusForbidden, "trading_disabled", "Trading is disabled for this demo.", requestID(r))
		return
	}
	if s.tradeApp == nil {
		writeError(w, http.StatusServiceUnavailable, "trading_unavailable", "Quick Trade is temporarily unavailable.", requestID(r))
		return
	}
	var input struct {
		QuoteID         string `json:"quoteId"`
		ClientRequestID string `json:"clientRequestId"`
	}
	if err := decodeBrowserJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "The confirmation request is invalid.", requestID(r))
		return
	}
	sess := sessionFrom(r)
	result, err := s.tradeApp.Confirm(r.Context(), quicktrade.ConfirmRequest{
		Identity: s.config.Username, SessionID: sess.ID, QuoteID: input.QuoteID, ClientRequestID: input.ClientRequestID,
	})
	if err != nil {
		s.writeTradeError(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusAccepted
	}
	writeJSON(w, status, map[string]any{"trade": toPublicTrade(result.Trade)})
}

func (s *Server) writeTradeError(w http.ResponseWriter, r *http.Request, err error) {
	var quoteErr *quicktrade.Error
	if errors.As(err, &quoteErr) {
		status := http.StatusConflict
		switch quoteErr.Code {
		case "INVALID_REQUEST", "INVALID_AMOUNT", "INVALID_CONFIRMATION", "UNSUPPORTED_ROUTE", "INVALID_QUOTE":
			status = http.StatusBadRequest
		case "DEMO_AMOUNT_LIMIT", "SESSION_TRADE_LIMIT", "HOURLY_TRADE_LIMIT":
			status = http.StatusTooManyRequests
		case "VENUE_UNAVAILABLE", "VENUE_WRITE_UNAVAILABLE":
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, strings.ToLower(quoteErr.Code), quoteErr.Error(), requestID(r))
		return
	}
	var confirmErr *intent.ConfirmError
	if errors.As(err, &confirmErr) {
		status := http.StatusConflict
		if confirmErr.Code == "SESSION_TRADE_LIMIT" || confirmErr.Code == "HOURLY_TRADE_LIMIT" {
			status = http.StatusTooManyRequests
		}
		writeError(w, status, strings.ToLower(confirmErr.Code), confirmErr.Error(), requestID(r))
		return
	}
	s.internalError(w, r, "quick trade", err)
}

func decodeBrowserJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if !strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]), "application/json") {
		return errors.New("JSON content type is required")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request must contain one JSON value")
	}
	return nil
}

func toPublicQuote(quote intent.QuickTradeQuote) publicQuote {
	return publicQuote{
		ID: quote.ID, Venue: quote.Venue, Environment: quote.Environment, AccountAlias: quote.AccountAlias,
		RouteID: quote.RouteID, FromAsset: quote.FromAsset, ToAsset: quote.ToAsset, SpendBudget: quote.SpendBudget,
		Instrument: quote.Instrument, Side: quote.Side, BaseQty: quote.BaseQty, LimitPrice: quote.LimitPrice,
		TimeInForce: quote.TimeInForce, ReferenceBid: quote.ReferenceBid, ReferenceAsk: quote.ReferenceAsk,
		BookObservedAt: quote.BookObservedAt.UTC().Format(time.RFC3339Nano), MetadataRevision: quote.MetadataRevision,
		PriceProtectionBPS: quote.PriceProtectionBPS, GrossReceiveEstimate: quote.GrossReceiveEstimate,
		NetReceiveEstimate: quote.NetReceiveEstimate, EstimatedFees: append([]intent.QuoteFee(nil), quote.EstimatedFees...),
		SourceDebitUpperBound: quote.SourceDebitUpperBound, ThirdAssetReserves: append([]intent.AssetReserve(nil), quote.ThirdAssetReserves...),
		AccountRevision: quote.AccountRevision, CreatedAt: quote.CreatedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt: quote.ExpiresAt.UTC().Format(time.RFC3339Nano), Warnings: append([]string(nil), quote.Warnings...),
		Executable: quote.Executable, BlockedReason: quote.BlockedReason,
	}
}
