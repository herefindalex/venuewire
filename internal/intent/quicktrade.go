package intent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type TradeStatus string

const (
	TradeCreated         TradeStatus = "Created"
	TradePendingSubmit   TradeStatus = "PendingSubmit"
	TradeSubmitted       TradeStatus = "Submitted"
	TradeAccepted        TradeStatus = "Accepted"
	TradePartiallyFilled TradeStatus = "PartiallyFilled"
	TradeFilled          TradeStatus = "Filled"
	TradePendingCancel   TradeStatus = "PendingCancel"
	TradeCancelled       TradeStatus = "Cancelled"
	TradeRejected        TradeStatus = "Rejected"
	TradeUnknown         TradeStatus = "Unknown"
)

func (s TradeStatus) Active() bool {
	return s == TradeCreated || s == TradePendingSubmit || s == TradeSubmitted || s == TradeAccepted || s == TradePartiallyFilled || s == TradePendingCancel || s == TradeUnknown
}

func (s TradeStatus) Terminal() bool {
	return s == TradeFilled || s == TradeCancelled || s == TradeRejected
}

type TradeFee struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}

type QuoteFee struct {
	Asset            string `json:"asset"`
	EstimatedAmount  string `json:"estimatedAmount"`
	Source           string `json:"source"`
	CalculationBasis string `json:"calculationBasis"`
}

type AssetReserve struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}

type TradeLifecycleEvent struct {
	Status    TradeStatus `json:"status"`
	At        time.Time   `json:"at"`
	Detail    string      `json:"detail,omitempty"`
	RequestID string      `json:"requestId,omitempty"`
}

type QuickTradeQuote struct {
	ID                    string         `json:"id"`
	Identity              string         `json:"identity"`
	Venue                 string         `json:"venue"`
	Environment           string         `json:"environment"`
	AccountAlias          string         `json:"accountAlias"`
	RouteID               string         `json:"routeId"`
	FromAsset             string         `json:"fromAsset"`
	ToAsset               string         `json:"toAsset"`
	SpendBudget           string         `json:"spendBudget"`
	Instrument            string         `json:"instrument"`
	Side                  string         `json:"side"`
	BaseQty               string         `json:"baseQty"`
	LimitPrice            string         `json:"limitPrice"`
	TimeInForce           string         `json:"timeInForce"`
	ReferenceBid          string         `json:"referenceBid"`
	ReferenceAsk          string         `json:"referenceAsk"`
	BookObservedAt        time.Time      `json:"bookObservedAt"`
	MetadataRevision      string         `json:"metadataRevision"`
	PriceProtectionBPS    int            `json:"priceProtectionBps"`
	GrossReceiveEstimate  string         `json:"grossReceiveEstimate"`
	NetReceiveEstimate    string         `json:"netReceiveEstimate,omitempty"`
	EstimatedFees         []QuoteFee     `json:"fees,omitempty"`
	SourceDebitUpperBound string         `json:"sourceDebitUpperBound"`
	ThirdAssetReserves    []AssetReserve `json:"thirdAssetReserves,omitempty"`
	AccountRevision       uint64         `json:"accountRevision"`
	CreatedAt             time.Time      `json:"createdAt"`
	ExpiresAt             time.Time      `json:"expiresAt"`
	Warnings              []string       `json:"warnings,omitempty"`
	Executable            bool           `json:"executable"`
	BlockedReason         string         `json:"blockedReason,omitempty"`
}

type QuickTrade struct {
	ID                       string                `json:"id"`
	Identity                 string                `json:"identity"`
	SessionID                string                `json:"sessionId"`
	ClientRequestID          string                `json:"clientRequestId"`
	Quote                    QuickTradeQuote       `json:"quote"`
	ClientOrderID            string                `json:"clientOrderId"`
	VenueOrderID             string                `json:"venueOrderId,omitempty"`
	Status                   TradeStatus           `json:"status"`
	RawVenueStatus           string                `json:"rawVenueStatus,omitempty"`
	ResultStatus             string                `json:"resultStatus,omitempty"`
	SendAttempted            bool                  `json:"sendAttempted"`
	FilledBaseQty            string                `json:"filledBaseQty,omitempty"`
	AveragePrice             string                `json:"averagePrice,omitempty"`
	GrossSourceSpent         string                `json:"grossSourceSpent,omitempty"`
	GrossDestinationReceived string                `json:"grossDestinationReceived,omitempty"`
	Fees                     []TradeFee            `json:"fees,omitempty"`
	NetDestinationReceived   string                `json:"netDestinationReceived,omitempty"`
	ActualSourceDebit        string                `json:"actualSourceDebit,omitempty"`
	FillDetailsStatus        string                `json:"fillDetailsStatus"`
	FeeDetailsStatus         string                `json:"feeDetailsStatus"`
	BalanceSyncStatus        string                `json:"balanceSyncStatus"`
	CreatedAt                time.Time             `json:"createdAt"`
	UpdatedAt                time.Time             `json:"updatedAt"`
	SubmittedAt              *time.Time            `json:"submittedAt,omitempty"`
	TerminalAt               *time.Time            `json:"terminalAt,omitempty"`
	LastCheckedAt            *time.Time            `json:"lastCheckedAt,omitempty"`
	LastPublicError          string                `json:"lastPublicError,omitempty"`
	Lifecycle                []TradeLifecycleEvent `json:"lifecycle"`
}

type QuickTradeConfirmation struct {
	IntentID        string
	Identity        string
	SessionID       string
	ClientRequestID string
	ClientOrderID   string
	Quote           QuickTradeQuote
}

type DemoLimits struct {
	MaxTradesPerSession int
	MaxTradesPerHour    int
	MaxConcurrentTrades int
}

type ConfirmError struct {
	Code string
	Err  error
}

func (e *ConfirmError) Error() string { return e.Err.Error() }
func (e *ConfirmError) Unwrap() error { return e.Err }

func (s Store) ConfirmQuickTrade(ctx context.Context, confirmation QuickTradeConfirmation, limits DemoLimits, now time.Time) (QuickTrade, bool, error) {
	if err := validateConfirmation(confirmation, limits, now); err != nil {
		return QuickTrade{}, false, err
	}
	var result QuickTrade
	created := false
	err := s.update(ctx, func(snapshot *Snapshot) error {
		initializeQuickTradeMaps(snapshot)
		requestKey := quickRequestKey(confirmation.Identity, confirmation.ClientRequestID)
		if intentID := snapshot.QuickRequests[requestKey]; intentID != "" {
			existing, ok := snapshot.QuickTrades[intentID]
			if !ok {
				return errors.New("quick trade request index is inconsistent")
			}
			if existing.Quote.ID != confirmation.Quote.ID {
				return &ConfirmError{Code: "IDEMPOTENCY_CONFLICT", Err: errors.New("client request ID was already used for another quote")}
			}
			result = existing
			return nil
		}
		if intentID := snapshot.ConsumedQuotes[confirmation.Quote.ID]; intentID != "" {
			existing, ok := snapshot.QuickTrades[intentID]
			if !ok {
				return errors.New("consumed quote index is inconsistent")
			}
			snapshot.QuickRequests[requestKey] = intentID
			result = existing
			return nil
		}
		if !now.Before(confirmation.Quote.ExpiresAt) {
			return &ConfirmError{Code: "QUOTE_EXPIRED", Err: errors.New("quote expired")}
		}
		if _, exists := snapshot.QuickTrades[confirmation.IntentID]; exists {
			return &ConfirmError{Code: "INTENT_ID_CONFLICT", Err: errors.New("trade intent ID already exists")}
		}

		hourCutoff := now.Add(-time.Hour)
		sessionCount, hourCount, activeCount := 0, 0, 0
		for _, trade := range snapshot.QuickTrades {
			if trade.SessionID == confirmation.SessionID {
				sessionCount++
			}
			if !trade.CreatedAt.Before(hourCutoff) {
				hourCount++
			}
			if trade.Status.Active() {
				activeCount++
			}
		}
		if sessionCount >= limits.MaxTradesPerSession {
			return &ConfirmError{Code: "SESSION_TRADE_LIMIT", Err: errors.New("session trade limit reached")}
		}
		if hourCount >= limits.MaxTradesPerHour {
			return &ConfirmError{Code: "HOURLY_TRADE_LIMIT", Err: errors.New("rolling hourly trade limit reached")}
		}
		if activeCount >= limits.MaxConcurrentTrades {
			return &ConfirmError{Code: "ACCOUNT_BUSY", Err: errors.New("another demo trade is unresolved")}
		}

		result = QuickTrade{
			ID:                confirmation.IntentID,
			Identity:          confirmation.Identity,
			SessionID:         confirmation.SessionID,
			ClientRequestID:   confirmation.ClientRequestID,
			Quote:             confirmation.Quote,
			ClientOrderID:     confirmation.ClientOrderID,
			Status:            TradeCreated,
			FillDetailsStatus: "pending",
			FeeDetailsStatus:  "pending",
			BalanceSyncStatus: "pending",
			CreatedAt:         now,
			UpdatedAt:         now,
			Lifecycle:         []TradeLifecycleEvent{{Status: TradeCreated, At: now}},
		}
		snapshot.QuickTrades[result.ID] = result
		snapshot.QuickRequests[requestKey] = result.ID
		snapshot.ConsumedQuotes[result.Quote.ID] = result.ID
		created = true
		return nil
	})
	return result, created, err
}

func (s Store) MarkQuickTradeDispatching(ctx context.Context, intentID string, now time.Time) (QuickTrade, error) {
	return s.updateQuickTrade(ctx, intentID, func(trade *QuickTrade) error {
		if trade.SendAttempted {
			return nil
		}
		if trade.Status != TradeCreated {
			return fmt.Errorf("cannot dispatch trade in status %s", trade.Status)
		}
		trade.SendAttempted = true
		return applyTradeStatus(trade, TradePendingSubmit, "", "", now)
	})
}

type QuickTradeUpdate struct {
	Status                   TradeStatus
	RawVenueStatus           string
	ResultStatus             string
	VenueOrderID             string
	FilledBaseQty            string
	AveragePrice             string
	GrossSourceSpent         string
	GrossDestinationReceived string
	Fees                     []TradeFee
	NetDestinationReceived   string
	ActualSourceDebit        string
	FillDetailsStatus        string
	FeeDetailsStatus         string
	BalanceSyncStatus        string
	PublicError              string
	ClearPublicError         bool
	LifecycleDetail          string
	RequestID                string
	Checked                  bool
}

func (s Store) UpdateQuickTrade(ctx context.Context, intentID string, update QuickTradeUpdate, now time.Time) (QuickTrade, error) {
	return s.updateQuickTrade(ctx, intentID, func(trade *QuickTrade) error {
		if update.Status != "" {
			detail := update.LifecycleDetail
			if detail == "" {
				detail = update.PublicError
			}
			if err := applyTradeStatus(trade, update.Status, detail, update.RequestID, now); err != nil {
				return err
			}
		}
		if update.RawVenueStatus != "" {
			trade.RawVenueStatus = update.RawVenueStatus
		}
		if update.ResultStatus != "" {
			trade.ResultStatus = update.ResultStatus
		}
		if update.VenueOrderID != "" {
			trade.VenueOrderID = update.VenueOrderID
		}
		copyIfSet(&trade.FilledBaseQty, update.FilledBaseQty)
		copyIfSet(&trade.AveragePrice, update.AveragePrice)
		copyIfSet(&trade.GrossSourceSpent, update.GrossSourceSpent)
		copyIfSet(&trade.GrossDestinationReceived, update.GrossDestinationReceived)
		copyIfSet(&trade.NetDestinationReceived, update.NetDestinationReceived)
		copyIfSet(&trade.ActualSourceDebit, update.ActualSourceDebit)
		copyIfSet(&trade.FillDetailsStatus, update.FillDetailsStatus)
		copyIfSet(&trade.FeeDetailsStatus, update.FeeDetailsStatus)
		copyIfSet(&trade.BalanceSyncStatus, update.BalanceSyncStatus)
		if update.Fees != nil {
			trade.Fees = append([]TradeFee(nil), update.Fees...)
		}
		if update.ClearPublicError {
			trade.LastPublicError = ""
		} else if update.PublicError != "" {
			trade.LastPublicError = update.PublicError
		}
		if update.Checked {
			checked := now
			trade.LastCheckedAt = &checked
		}
		trade.UpdatedAt = now
		return nil
	})
}

func (s Store) GetQuickTrade(ctx context.Context, intentID string) (QuickTrade, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return QuickTrade{}, err
	}
	trade, ok := snapshot.QuickTrades[intentID]
	if !ok {
		return QuickTrade{}, errors.New("quick trade intent not found")
	}
	return trade, nil
}

func (s Store) GetQuickTradeByRequest(ctx context.Context, identity, clientRequestID string) (QuickTrade, bool, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return QuickTrade{}, false, err
	}
	intentID := snapshot.QuickRequests[quickRequestKey(identity, clientRequestID)]
	if intentID == "" {
		return QuickTrade{}, false, nil
	}
	trade, ok := snapshot.QuickTrades[intentID]
	if !ok {
		return QuickTrade{}, false, errors.New("quick trade request index is inconsistent")
	}
	return trade, true, nil
}

func (s Store) GetQuickTradeByQuote(ctx context.Context, quoteID string) (QuickTrade, bool, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return QuickTrade{}, false, err
	}
	intentID := snapshot.ConsumedQuotes[quoteID]
	if intentID == "" {
		return QuickTrade{}, false, nil
	}
	trade, ok := snapshot.QuickTrades[intentID]
	if !ok {
		return QuickTrade{}, false, errors.New("consumed quote index is inconsistent")
	}
	return trade, true, nil
}

func (s Store) ListQuickTrades(ctx context.Context) ([]QuickTrade, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]QuickTrade, 0, len(snapshot.QuickTrades))
	for _, trade := range snapshot.QuickTrades {
		result = append(result, trade)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (s Store) updateQuickTrade(ctx context.Context, intentID string, mutate func(*QuickTrade) error) (QuickTrade, error) {
	var result QuickTrade
	err := s.update(ctx, func(snapshot *Snapshot) error {
		initializeQuickTradeMaps(snapshot)
		trade, ok := snapshot.QuickTrades[intentID]
		if !ok {
			return errors.New("quick trade intent not found")
		}
		if err := mutate(&trade); err != nil {
			return err
		}
		snapshot.QuickTrades[intentID] = trade
		result = trade
		return nil
	})
	return result, err
}

func initializeQuickTradeMaps(snapshot *Snapshot) {
	if snapshot.QuickTrades == nil {
		snapshot.QuickTrades = make(map[string]QuickTrade)
	}
	if snapshot.QuickRequests == nil {
		snapshot.QuickRequests = make(map[string]string)
	}
	if snapshot.ConsumedQuotes == nil {
		snapshot.ConsumedQuotes = make(map[string]string)
	}
}

func quickRequestKey(identity, requestID string) string {
	return strings.TrimSpace(identity) + "\x00" + strings.TrimSpace(requestID)
}

func copyIfSet(target *string, value string) {
	if value != "" {
		*target = value
	}
}

func validateConfirmation(value QuickTradeConfirmation, limits DemoLimits, now time.Time) error {
	for name, field := range map[string]string{
		"intent ID": value.IntentID, "identity": value.Identity, "session ID": value.SessionID,
		"client request ID": value.ClientRequestID, "client order ID": value.ClientOrderID,
		"quote ID": value.Quote.ID, "quote identity": value.Quote.Identity,
	} {
		if strings.TrimSpace(field) == "" {
			return &ConfirmError{Code: "INVALID_CONFIRMATION", Err: fmt.Errorf("%s is required", name)}
		}
	}
	if value.Identity != value.Quote.Identity {
		return &ConfirmError{Code: "QUOTE_OWNERSHIP_MISMATCH", Err: errors.New("quote does not belong to the authenticated identity")}
	}
	if !value.Quote.Executable {
		return &ConfirmError{Code: "QUOTE_NOT_EXECUTABLE", Err: errors.New("quote is not executable")}
	}
	if limits.MaxTradesPerSession <= 0 || limits.MaxTradesPerHour <= 0 || limits.MaxConcurrentTrades <= 0 {
		return &ConfirmError{Code: "INVALID_DEMO_LIMITS", Err: errors.New("demo trade limits must be positive")}
	}
	return nil
}

func applyTradeStatus(trade *QuickTrade, next TradeStatus, detail, requestID string, now time.Time) error {
	if trade.Status == next {
		return nil
	}
	if !validTradeTransition(trade.Status, next) {
		return fmt.Errorf("invalid quick trade transition %s -> %s", trade.Status, next)
	}
	trade.Status = next
	trade.UpdatedAt = now
	if (next == TradeSubmitted || next == TradeAccepted || next == TradePartiallyFilled || next == TradeFilled) && trade.SubmittedAt == nil {
		submitted := now
		trade.SubmittedAt = &submitted
	}
	if next.Terminal() {
		terminal := now
		trade.TerminalAt = &terminal
	}
	trade.Lifecycle = append(trade.Lifecycle, TradeLifecycleEvent{Status: next, At: now, Detail: detail, RequestID: requestID})
	return nil
}

func validTradeTransition(from, to TradeStatus) bool {
	if (from == TradePendingCancel || from == TradeCancelled) && to == TradeFilled {
		return true
	}
	allowed := map[TradeStatus]map[TradeStatus]bool{
		TradeCreated:         {TradePendingSubmit: true, TradeRejected: true},
		TradePendingSubmit:   {TradeSubmitted: true, TradeAccepted: true, TradePartiallyFilled: true, TradeFilled: true, TradeRejected: true, TradeUnknown: true},
		TradeSubmitted:       {TradeAccepted: true, TradePartiallyFilled: true, TradeFilled: true, TradeRejected: true, TradeUnknown: true},
		TradeAccepted:        {TradePartiallyFilled: true, TradeFilled: true, TradePendingCancel: true, TradeCancelled: true, TradeRejected: true, TradeUnknown: true},
		TradePartiallyFilled: {TradeFilled: true, TradePendingCancel: true, TradeCancelled: true, TradeUnknown: true},
		TradePendingCancel:   {TradeCancelled: true, TradeUnknown: true},
		TradeUnknown:         {TradeSubmitted: true, TradeAccepted: true, TradePartiallyFilled: true, TradeFilled: true, TradeCancelled: true, TradeRejected: true},
	}
	return allowed[from][to]
}
