package quicktrade

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"venuewire/internal/domain"
	"venuewire/internal/intent"
)

type Submitter interface {
	Submit(context.Context, intent.QuickTrade) (Submission, error)
}

type Submission struct {
	VenueOrderID   string
	RawVenueStatus string
	Accepted       bool
}

type SubmissionObservation struct {
	Venue         domain.Venue
	IntentID      string
	ClientOrderID string
	VenueOrderID  string
	AckAt         time.Time
	RequestRTT    time.Duration
	Err           error
}

type RejectedError struct {
	PublicMessage string
	Err           error
}

func (e *RejectedError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.PublicMessage
}

func (e *RejectedError) Unwrap() error { return e.Err }

type QuoteCache struct {
	mu     sync.Mutex
	max    int
	quotes map[string]intent.QuickTradeQuote
	order  []string
}

func NewQuoteCache(maxEntries int) *QuoteCache {
	if maxEntries <= 0 {
		maxEntries = 1_000
	}
	return &QuoteCache{max: maxEntries, quotes: make(map[string]intent.QuickTradeQuote)}
}

func (c *QuoteCache) Put(quote intent.QuickTradeQuote, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(now)
	if _, exists := c.quotes[quote.ID]; !exists {
		c.order = append(c.order, quote.ID)
	}
	c.quotes[quote.ID] = quote
	for len(c.quotes) > c.max && len(c.order) > 0 {
		delete(c.quotes, c.order[0])
		c.order = c.order[1:]
	}
}

func (c *QuoteCache) Get(id, identity string, now time.Time) (intent.QuickTradeQuote, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(now)
	quote, ok := c.quotes[id]
	if !ok || quote.Identity != identity {
		return intent.QuickTradeQuote{}, false
	}
	return quote, true
}

func (c *QuoteCache) prune(now time.Time) {
	kept := c.order[:0]
	for _, id := range c.order {
		quote, ok := c.quotes[id]
		if !ok {
			continue
		}
		if !now.Before(quote.ExpiresAt) {
			delete(c.quotes, id)
			continue
		}
		kept = append(kept, id)
	}
	c.order = kept
}

type Application struct {
	Quotes       *Service
	Cache        *QuoteCache
	Store        intent.Store
	Submitters   map[domain.Venue]Submitter
	Limits       intent.DemoLimits
	Now          func() time.Time
	NewIntentID  func() (string, error)
	OnSubmission func(SubmissionObservation)
}

type ConfirmRequest struct {
	Identity        string
	SessionID       string
	QuoteID         string
	ClientRequestID string
}

type ConfirmResult struct {
	Trade   intent.QuickTrade
	Created bool
}

func (a *Application) CreateQuote(ctx context.Context, request CreateRequest) (intent.QuickTradeQuote, error) {
	if a.Quotes == nil || a.Cache == nil {
		return intent.QuickTradeQuote{}, errors.New("quote service is unavailable")
	}
	quote, err := a.Quotes.Create(ctx, request)
	if err != nil {
		return intent.QuickTradeQuote{}, err
	}
	a.Cache.Put(quote, a.now())
	return quote, nil
}

func (a *Application) Confirm(ctx context.Context, request ConfirmRequest) (ConfirmResult, error) {
	ctx = context.WithoutCancel(ctx)
	if strings.TrimSpace(request.Identity) == "" || strings.TrimSpace(request.SessionID) == "" || strings.TrimSpace(request.QuoteID) == "" || strings.TrimSpace(request.ClientRequestID) == "" {
		return ConfirmResult{}, quoteError("INVALID_CONFIRMATION", "quoteId and clientRequestId are required")
	}
	if len(request.ClientRequestID) > 128 {
		return ConfirmResult{}, quoteError("INVALID_CONFIRMATION", "clientRequestId is too long")
	}
	if existing, ok, err := a.Store.GetQuickTradeByRequest(ctx, request.Identity, request.ClientRequestID); err != nil {
		return ConfirmResult{}, err
	} else if ok {
		if existing.Quote.ID != request.QuoteID {
			return ConfirmResult{}, &intent.ConfirmError{Code: "IDEMPOTENCY_CONFLICT", Err: errors.New("client request ID was already used for another quote")}
		}
		return ConfirmResult{Trade: existing}, nil
	}
	if existing, ok, err := a.Store.GetQuickTradeByQuote(ctx, request.QuoteID); err != nil {
		return ConfirmResult{}, err
	} else if ok {
		return ConfirmResult{Trade: existing}, nil
	}
	now := a.now()
	quote, ok := a.Cache.Get(request.QuoteID, request.Identity, now)
	if !ok {
		return ConfirmResult{}, quoteError("QUOTE_EXPIRED", "the quote is missing or expired; review a new quote")
	}
	venue := domain.Venue(quote.Venue)
	submitter := a.Submitters[venue]
	if submitter == nil {
		return ConfirmResult{}, quoteError("VENUE_WRITE_UNAVAILABLE", "trading is unavailable for the selected venue")
	}
	if err := a.Quotes.ValidateForConfirm(ctx, quote); err != nil {
		return ConfirmResult{}, err
	}
	intentID, err := a.intentID()
	if err != nil {
		return ConfirmResult{}, err
	}
	confirmation := intent.QuickTradeConfirmation{
		IntentID: intentID, Identity: request.Identity, SessionID: request.SessionID,
		ClientRequestID: request.ClientRequestID, ClientOrderID: clientOrderID(intentID), Quote: quote,
	}
	trade, created, err := a.Store.ConfirmQuickTrade(ctx, confirmation, a.Limits, now)
	if err != nil || !created {
		return ConfirmResult{Trade: trade, Created: created}, err
	}
	trade, err = a.Store.MarkQuickTradeDispatching(ctx, trade.ID, a.now())
	if err != nil {
		return ConfirmResult{Trade: trade, Created: true}, err
	}

	submissionStarted := time.Now()
	submission, submitErr := submitter.Submit(context.WithoutCancel(ctx), trade)
	if a.OnSubmission != nil {
		a.OnSubmission(SubmissionObservation{
			Venue: venue, IntentID: trade.ID, ClientOrderID: trade.ClientOrderID, VenueOrderID: submission.VenueOrderID,
			AckAt: a.now(), RequestRTT: time.Since(submissionStarted), Err: submitErr,
		})
	}
	if submitErr != nil {
		var rejected *RejectedError
		if errors.As(submitErr, &rejected) {
			message := rejected.PublicMessage
			if message == "" {
				message = "The venue rejected the order."
			}
			trade, err = a.Store.UpdateQuickTrade(context.WithoutCancel(ctx), trade.ID, intent.QuickTradeUpdate{Status: intent.TradeRejected, ResultStatus: "REJECTED", PublicError: message}, a.now())
			return ConfirmResult{Trade: trade, Created: true}, err
		}
		trade, err = a.Store.UpdateQuickTrade(context.WithoutCancel(ctx), trade.ID, intent.QuickTradeUpdate{Status: intent.TradeUnknown, ResultStatus: "OUTCOME_UNKNOWN", PublicError: "The exchange result could not be confirmed."}, a.now())
		return ConfirmResult{Trade: trade, Created: true}, err
	}
	status := intent.TradeSubmitted
	if submission.Accepted {
		status = intent.TradeAccepted
	}
	trade, err = a.Store.UpdateQuickTrade(context.WithoutCancel(ctx), trade.ID, intent.QuickTradeUpdate{Status: status, VenueOrderID: submission.VenueOrderID, RawVenueStatus: submission.RawVenueStatus}, a.now())
	return ConfirmResult{Trade: trade, Created: true}, err
}

func (a *Application) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Application) intentID() (string, error) {
	if a.NewIntentID != nil {
		return a.NewIntentID()
	}
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate trade intent ID: %w", err)
	}
	return "trade_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)), nil
}

func clientOrderID(intentID string) string {
	value := "vw-" + strings.TrimPrefix(intentID, "trade_")
	if len(value) > 36 {
		return value[:36]
	}
	return value
}
