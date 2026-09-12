package quicktrade

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/herefindalex/venuewire/internal/accountstate"
	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/intent"
)

func TestApplicationConcurrentConfirmSubmitsExactlyOnce(t *testing.T) {
	now := time.Now().UTC()
	submitter := &recordingSubmitter{result: Submission{VenueOrderID: "venue-order-1", RawVenueStatus: "New", Accepted: true}}
	application := fixtureApplication(t, now, submitter)
	observations := make(chan SubmissionObservation, 1)
	application.OnSubmission = func(observation SubmissionObservation) { observations <- observation }
	quote, err := application.CreateQuote(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "100", AccountAlias: "bybit-test"})
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan ConfirmResult, 2)
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, confirmErr := application.Confirm(context.Background(), ConfirmRequest{Identity: "shared-user", SessionID: "session-1", QuoteID: quote.ID, ClientRequestID: "request-1"})
			results <- result
			errorsFound <- confirmErr
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	var intentID string
	for result := range results {
		if intentID == "" {
			intentID = result.Trade.ID
		} else if result.Trade.ID != intentID {
			t.Fatalf("duplicate intents: %q and %q", intentID, result.Trade.ID)
		}
	}
	if submitter.Calls() != 1 {
		t.Fatalf("submit calls = %d, want 1", submitter.Calls())
	}
	observation := <-observations
	if observation.Venue != domain.VenueBybit || observation.IntentID != intentID || observation.VenueOrderID != "venue-order-1" || observation.Err != nil || !observation.AckAt.Equal(now) || observation.RequestRTT < 0 {
		t.Fatalf("submission observation = %+v", observation)
	}
	stored, err := application.Store.GetQuickTrade(context.Background(), intentID)
	if err != nil || stored.Status != intent.TradeAccepted || stored.VenueOrderID != "venue-order-1" {
		t.Fatalf("stored trade = %+v err=%v", stored, err)
	}
}

func TestApplicationUncertainSubmissionSurvivesRestartAndIsNotResent(t *testing.T) {
	now := time.Now().UTC()
	submitter := &recordingSubmitter{err: errors.New("fixture timeout")}
	application := fixtureApplication(t, now, submitter)
	quote, err := application.CreateQuote(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "100", AccountAlias: "bybit-test"})
	if err != nil {
		t.Fatal(err)
	}
	request := ConfirmRequest{Identity: "shared-user", SessionID: "session-1", QuoteID: quote.ID, ClientRequestID: "request-1"}
	first, err := application.Confirm(context.Background(), request)
	if err != nil || first.Trade.Status != intent.TradeUnknown || !first.Trade.SendAttempted {
		t.Fatalf("first result = %+v err=%v", first, err)
	}

	restartedSubmitter := &recordingSubmitter{}
	restarted := fixtureApplicationWithPath(now.Add(time.Minute), restartedSubmitter, application.Store.Path)
	retry, err := restarted.Confirm(context.Background(), request)
	if err != nil || retry.Trade.ID != first.Trade.ID || retry.Trade.Status != intent.TradeUnknown {
		t.Fatalf("restart retry = %+v err=%v", retry, err)
	}
	if restartedSubmitter.Calls() != 0 {
		t.Fatalf("restart resent uncertain order %d times", restartedSubmitter.Calls())
	}
}

func TestApplicationDistinguishesDefiniteRejectionFromUnknown(t *testing.T) {
	now := time.Now().UTC()
	submitter := &recordingSubmitter{err: &RejectedError{PublicMessage: "The venue rejected the quantity.", Err: errors.New("fixture native reject")}}
	application := fixtureApplication(t, now, submitter)
	quote, err := application.CreateQuote(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "100", AccountAlias: "bybit-test"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.Confirm(context.Background(), ConfirmRequest{Identity: "shared-user", SessionID: "session-1", QuoteID: quote.ID, ClientRequestID: "request-1"})
	if err != nil || result.Trade.Status != intent.TradeRejected || result.Trade.ResultStatus != "REJECTED" || result.Trade.LastPublicError != "The venue rejected the quantity." {
		t.Fatalf("rejection result = %+v err=%v", result, err)
	}
}

func TestApplicationSubmissionOutlivesBrowserCancellation(t *testing.T) {
	now := time.Now().UTC()
	submitter := &recordingSubmitter{result: Submission{VenueOrderID: "venue-order-1"}, requireLiveContext: true}
	application := fixtureApplication(t, now, submitter)
	quote, err := application.CreateQuote(context.Background(), CreateRequest{Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "100", AccountAlias: "bybit-test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := application.Confirm(ctx, ConfirmRequest{Identity: "shared-user", SessionID: "session-1", QuoteID: quote.ID, ClientRequestID: "request-1"})
	if err != nil || result.Trade.Status != intent.TradeSubmitted {
		t.Fatalf("cancelled browser result = %+v err=%v", result, err)
	}
}

func TestApplicationPrivateDisconnectBeforeAckDoesNotLoseSubmission(t *testing.T) {
	now := time.Date(2026, 9, 10, 18, 30, 0, 0, time.UTC)
	accounts, err := accountstate.NewManager([]accountstate.Provider{applicationAccountProvider{}}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	submitter := &recordingSubmitter{
		result: Submission{VenueOrderID: "venue-order-after-disconnect", RawVenueStatus: "New", Accepted: true},
		beforeReturn: func() {
			accounts.UpdatePrivateWS(domain.VenueBybit, "RECONNECTING", time.Time{}, 1)
		},
	}
	application := fixtureApplication(t, now, submitter)
	application.OnSubmission = func(observation SubmissionObservation) {
		accounts.RecordOrderSubmission(observation.Venue, observation.ClientOrderID, observation.VenueOrderID, observation.AckAt, observation.RequestRTT, observation.Err == nil, false)
	}
	quote, err := application.CreateQuote(context.Background(), CreateRequest{
		Identity: "shared-user", Venue: domain.VenueBybit, RouteID: "bybit-usdt-btc", SpendBudget: "100", AccountAlias: "bybit-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.Confirm(context.Background(), ConfirmRequest{
		Identity: "shared-user", SessionID: "session-1", QuoteID: quote.ID, ClientRequestID: "request-disconnect-before-ack",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Trade.Status != intent.TradeAccepted || result.Trade.VenueOrderID != "venue-order-after-disconnect" || submitter.Calls() != 1 {
		t.Fatalf("submission after private disconnect = %+v calls=%d", result.Trade, submitter.Calls())
	}
	accounts.RecordOrderEvent(domain.VenueBybit, result.Trade.ClientOrderID, result.Trade.VenueOrderID, "order", now.Add(50*time.Millisecond))
	health := accounts.Health()[0]
	if health.PrivateWS != "RECONNECTING" || !health.HasFirstOrderEvent || health.FirstOrderEventLatency != 50*time.Millisecond {
		t.Fatalf("post-reconnect observation = %+v", health)
	}
}

type applicationAccountProvider struct{}

func (applicationAccountProvider) Venue() domain.Venue { return domain.VenueBybit }
func (applicationAccountProvider) Snapshot(context.Context) (accountstate.Snapshot, error) {
	return accountstate.Snapshot{}, nil
}

type recordingSubmitter struct {
	mu                 sync.Mutex
	calls              int
	result             Submission
	err                error
	requireLiveContext bool
	beforeReturn       func()
}

func (s *recordingSubmitter) Submit(ctx context.Context, _ intent.QuickTrade) (Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.requireLiveContext && ctx.Err() != nil {
		return Submission{}, errors.New("submission inherited browser cancellation")
	}
	if s.beforeReturn != nil {
		s.beforeReturn()
	}
	return s.result, s.err
}

func (s *recordingSubmitter) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func fixtureApplication(t *testing.T, now time.Time, submitter Submitter) *Application {
	t.Helper()
	return fixtureApplicationWithPath(now, submitter, filepath.Join(t.TempDir(), "intents.json"))
}

func fixtureApplicationWithPath(now time.Time, submitter Submitter, path string) *Application {
	providers := fixtureProviders(now)
	return &Application{
		Quotes:     fixtureService(now, providers),
		Cache:      NewQuoteCache(100),
		Store:      intent.Store{Path: path},
		Submitters: map[domain.Venue]Submitter{domain.VenueBybit: submitter},
		Limits:     intent.DemoLimits{MaxTradesPerSession: 10, MaxTradesPerHour: 30, MaxConcurrentTrades: 1},
		Now:        func() time.Time { return now },
		NewIntentID: func() (string, error) {
			return "trade_fixture", nil
		},
	}
}
