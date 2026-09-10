package webconsole

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"venuewire/internal/intent"
	"venuewire/internal/quicktrade"
)

type TradeRechecker interface {
	RecheckTrade(context.Context, string, time.Time) (intent.QuickTrade, error)
}

type recheckCoordinator struct {
	mu     sync.Mutex
	active map[string]bool
	last   map[string]time.Time
}

type publicTrade struct {
	ID                       string                       `json:"intentId"`
	ClientRequestID          string                       `json:"clientRequestId"`
	QuoteID                  string                       `json:"quoteId"`
	Venue                    string                       `json:"venue"`
	Environment              string                       `json:"environment"`
	AccountAlias             string                       `json:"accountAlias"`
	RouteID                  string                       `json:"routeId"`
	FromAsset                string                       `json:"fromAsset"`
	ToAsset                  string                       `json:"toAsset"`
	RequestedAmount          string                       `json:"requestedAmount"`
	Instrument               string                       `json:"instrument"`
	Side                     string                       `json:"side"`
	ClientOrderID            string                       `json:"clientOrderId"`
	VenueOrderID             string                       `json:"venueOrderId,omitempty"`
	Status                   intent.TradeStatus           `json:"status"`
	RawVenueStatus           string                       `json:"rawVenueStatus,omitempty"`
	ResultStatus             string                       `json:"resultStatus,omitempty"`
	SubmittedBaseQty         string                       `json:"submittedBaseQty"`
	SubmittedLimitPrice      string                       `json:"submittedLimitPrice"`
	SubmittedTimeInForce     string                       `json:"submittedTimeInForce"`
	FilledBaseQty            string                       `json:"filledBaseQty,omitempty"`
	AveragePrice             string                       `json:"averagePrice,omitempty"`
	GrossSourceSpent         string                       `json:"grossSourceSpent,omitempty"`
	GrossDestinationReceived string                       `json:"grossDestinationReceived,omitempty"`
	Fees                     []intent.TradeFee            `json:"fees,omitempty"`
	NetDestinationReceived   string                       `json:"netDestinationReceived,omitempty"`
	ActualSourceDebit        string                       `json:"actualSourceDebit,omitempty"`
	FillDetailsStatus        string                       `json:"fillDetailsStatus"`
	FeeDetailsStatus         string                       `json:"feeDetailsStatus"`
	BalanceSyncStatus        string                       `json:"balanceSyncStatus"`
	CreatedAt                time.Time                    `json:"createdAt"`
	UpdatedAt                time.Time                    `json:"updatedAt"`
	SubmittedAt              *time.Time                   `json:"submittedAt,omitempty"`
	TerminalAt               *time.Time                   `json:"terminalAt,omitempty"`
	LastCheckedAt            *time.Time                   `json:"lastCheckedAt,omitempty"`
	Message                  string                       `json:"message,omitempty"`
	Lifecycle                []intent.TradeLifecycleEvent `json:"lifecycle"`
}

func (s *Server) handleRecentTrades(w http.ResponseWriter, r *http.Request) {
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "The trade history limit must be from 1 to 100.", requestID(r))
			return
		}
		limit = parsed
	}
	trades, err := s.trades.ListQuickTrades(r.Context())
	if err != nil {
		s.internalError(w, r, "load recent trades", err)
		return
	}
	if len(trades) > limit {
		trades = trades[:limit]
	}
	result := make([]publicTrade, 0, len(trades))
	for _, trade := range trades {
		result = append(result, toPublicTrade(trade))
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": result})
}

func (s *Server) handleTradeDetail(w http.ResponseWriter, r *http.Request) {
	intentID := strings.TrimSpace(r.PathValue("intentID"))
	if intentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_trade_id", "A trade ID is required.", requestID(r))
		return
	}
	trade, err := s.trades.GetQuickTrade(r.Context(), intentID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "trade_not_found", "The trade was not found.", requestID(r))
			return
		}
		s.internalError(w, r, "load trade", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trade": toPublicTrade(trade)})
}

func (s *Server) handleTradeRecheck(w http.ResponseWriter, r *http.Request) {
	intentID := strings.TrimSpace(r.PathValue("intentID"))
	if intentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_trade_id", "A trade ID is required.", requestID(r))
		return
	}
	if _, err := s.trades.GetQuickTrade(r.Context(), intentID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "trade_not_found", "The trade was not found.", requestID(r))
			return
		}
		s.internalError(w, r, "load trade for recheck", err)
		return
	}
	if s.rechecker == nil {
		writeError(w, http.StatusServiceUnavailable, "recheck_unavailable", "Order recheck is temporarily unavailable.", requestID(r))
		return
	}
	now := s.now()
	status, retryAfter := s.rechecks.begin(intentID, now)
	switch status {
	case "active":
		trade, err := s.trades.GetQuickTrade(r.Context(), intentID)
		if err != nil {
			s.internalError(w, r, "load active recheck", err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"trade": toPublicTrade(trade), "recheckInProgress": true})
		return
	case "limited":
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Round(time.Second)/time.Second))))
		writeError(w, http.StatusTooManyRequests, "recheck_rate_limited", "Wait before checking this trade again.", requestID(r))
		return
	}
	defer s.rechecks.finish(intentID, now)

	trade, err := s.rechecker.RecheckTrade(r.Context(), intentID, now)
	if err != nil {
		var public *RecheckError
		if errors.As(err, &public) {
			writeError(w, public.Status, public.Code, public.Message, requestID(r))
			return
		}
		var venueError *quicktrade.Error
		if errors.As(err, &venueError) {
			writeError(w, http.StatusServiceUnavailable, strings.ToLower(venueError.Code), venueError.Error(), requestID(r))
			return
		}
		s.internalError(w, r, "recheck trade", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trade": toPublicTrade(trade), "recheckInProgress": false})
}

type RecheckError struct {
	Status  int
	Code    string
	Message string
	Err     error
}

func (e *RecheckError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func (e *RecheckError) Unwrap() error { return e.Err }

func (c *recheckCoordinator) begin(intentID string, now time.Time) (string, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active[intentID] {
		return "active", 0
	}
	if last := c.last[intentID]; !last.IsZero() && now.Sub(last) < 2*time.Second {
		return "limited", 2*time.Second - now.Sub(last)
	}
	c.active[intentID] = true
	return "started", 0
}

func (c *recheckCoordinator) finish(intentID string, now time.Time) {
	c.mu.Lock()
	delete(c.active, intentID)
	c.last[intentID] = now
	c.mu.Unlock()
}

func toPublicTrade(trade intent.QuickTrade) publicTrade {
	return publicTrade{
		ID: trade.ID, ClientRequestID: trade.ClientRequestID, QuoteID: trade.Quote.ID,
		Venue: trade.Quote.Venue, Environment: trade.Quote.Environment, AccountAlias: trade.Quote.AccountAlias,
		RouteID: trade.Quote.RouteID, FromAsset: trade.Quote.FromAsset, ToAsset: trade.Quote.ToAsset,
		RequestedAmount: trade.Quote.SpendBudget, Instrument: trade.Quote.Instrument, Side: trade.Quote.Side,
		ClientOrderID: trade.ClientOrderID, VenueOrderID: trade.VenueOrderID, Status: trade.Status,
		RawVenueStatus: trade.RawVenueStatus, ResultStatus: trade.ResultStatus,
		SubmittedBaseQty: trade.Quote.BaseQty, SubmittedLimitPrice: trade.Quote.LimitPrice,
		SubmittedTimeInForce: trade.Quote.TimeInForce, FilledBaseQty: trade.FilledBaseQty,
		AveragePrice: trade.AveragePrice, GrossSourceSpent: trade.GrossSourceSpent,
		GrossDestinationReceived: trade.GrossDestinationReceived, Fees: append([]intent.TradeFee(nil), trade.Fees...),
		NetDestinationReceived: trade.NetDestinationReceived, ActualSourceDebit: trade.ActualSourceDebit,
		FillDetailsStatus: trade.FillDetailsStatus, FeeDetailsStatus: trade.FeeDetailsStatus,
		BalanceSyncStatus: trade.BalanceSyncStatus, CreatedAt: trade.CreatedAt, UpdatedAt: trade.UpdatedAt,
		SubmittedAt: trade.SubmittedAt, TerminalAt: trade.TerminalAt, LastCheckedAt: trade.LastCheckedAt,
		Message: trade.LastPublicError, Lifecycle: append([]intent.TradeLifecycleEvent(nil), trade.Lifecycle...),
	}
}
