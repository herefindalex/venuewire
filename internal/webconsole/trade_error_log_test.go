package webconsole

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"venuewire/internal/intent"
	"venuewire/internal/observability"
	"venuewire/internal/quicktrade"
)

func TestTradeErrorLogIncludesRedactedProviderCause(t *testing.T) {
	var logs bytes.Buffer
	server := newTestServer(t)
	server.logger = observability.NewJSON(&logs, "fixture-api-secret")
	server.tradeApp = rejectingTradeApplication{err: &quicktrade.Error{
		Code:          "MARKET_UNAVAILABLE",
		PublicMessage: "Executable Spot market data is unavailable.",
		Err:           errors.New("upstream fixture-api-secret timed out"),
	}}

	login := performRequest(server, http.MethodPost, "/api/auth/login", `{"username":"alex","password":"fixture-password"}`, nil, "")
	var sessionView struct {
		CSRFToken string `json:"csrfToken"`
	}
	decodeBody(t, login, &sessionView)
	response := performRequest(server, http.MethodPost, "/api/venues/bybit/quotes", `{"routeId":"bybit-usdt-btc","amount":"10"}`, login.Result().Cookies()[0], sessionView.CSRFToken)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("quote status = %d body=%s", response.Code, response.Body.String())
	}

	output := logs.String()
	for _, expected := range []string{"trade request rejected", "MARKET_UNAVAILABLE", "upstream", "requestId"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("log missing %q: %s", expected, output)
		}
	}
	if strings.Contains(output, "fixture-api-secret") || strings.Contains(output, `"amount":"10"`) {
		t.Fatalf("trade rejection log exposed a secret or request body: %s", output)
	}
}

type rejectingTradeApplication struct {
	err error
}

func (a rejectingTradeApplication) CreateQuote(context.Context, quicktrade.CreateRequest) (intent.QuickTradeQuote, error) {
	return intent.QuickTradeQuote{}, a.err
}

func (rejectingTradeApplication) Confirm(context.Context, quicktrade.ConfirmRequest) (quicktrade.ConfirmResult, error) {
	return quicktrade.ConfirmResult{}, errors.New("unexpected confirm")
}
