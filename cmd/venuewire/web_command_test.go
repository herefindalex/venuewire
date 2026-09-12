package main

import (
	"errors"
	"testing"
	"time"

	"github.com/herefindalex/venuewire/internal/accountstate"
	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/quicktrade"
	"github.com/herefindalex/venuewire/internal/rest"
)

func TestSubmissionRateLimitedRecognizesVenueSignals(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "Bybit HTTP", err: &rest.APIError{HTTPStatus: 429}, want: true},
		{name: "Bybit code", err: &rest.APIError{HTTPStatus: 400, Code: 10006}, want: true},
		{name: "Deribit code", err: &deribit.RPCError{Code: 10028}, want: true},
		{name: "wrapped venue rejection", err: &quicktrade.RejectedError{Err: &deribit.RPCError{Code: 10028}}, want: true},
		{name: "ordinary rejection", err: &quicktrade.RejectedError{Err: errors.New("fixture rejection")}},
		{name: "success"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := submissionRateLimited(test.err); got != test.want {
				t.Fatalf("submissionRateLimited() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestRecordSubmissionObservationPublishesRateLimitHealth(t *testing.T) {
	manager, err := accountstate.NewManager([]accountstate.Provider{streamMetricProvider{venue: domain.VenueBybit}}, time.Second, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ackAt := time.Date(2026, 9, 10, 19, 0, 0, 0, time.UTC)
	recordSubmissionObservation(manager, quicktrade.SubmissionObservation{
		Venue: domain.VenueBybit, ClientOrderID: "vw-rate-limited", AckAt: ackAt,
		RequestRTT: 25 * time.Millisecond,
		Err:        &quicktrade.RejectedError{Err: &rest.APIError{Code: 10006}},
	})
	health := manager.Health()[0]
	if health.RateLimitState != "LIMITED" || health.OrderRequestErrors != 1 || health.OrderRequestRTT != 25*time.Millisecond {
		t.Fatalf("rate-limit health = %+v", health)
	}
}
