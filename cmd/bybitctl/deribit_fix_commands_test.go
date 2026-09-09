package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"bybit/internal/config"
)

func TestDeribitFIXMockDemoProvidesSessionEvidenceWithoutCredentials(t *testing.T) {
	cfg := config.Config{Deribit: config.DeribitConfig{AccountAlias: "fixture-account"}}
	var output bytes.Buffer
	handled, err := executeDeribitFIXCommand(
		context.Background(),
		cfg,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		[]string{"venue", "deribit", "fix", "mock-demo"},
		&output,
	)
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	var result struct {
		Venue                  string `json:"venue"`
		ValidationLevel        string `json:"validationLevel"`
		Authenticated          bool   `json:"authenticated"`
		HeartbeatTestRequest   bool   `json:"heartbeatTestRequest"`
		ResendSequenceReset    bool   `json:"resendSequenceReset"`
		ReconciliationCallback bool   `json:"reconciliationCallback"`
		MockFinalOrderStatus    string `json:"mockFinalOrderStatus"`
		SecurityListMultiplier  string `json:"securityListMultiplier"`
		TradingWritePerformed  bool   `json:"tradingWritePerformed"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Venue != "deribit" || result.ValidationLevel != "LOCAL_TESTED" || !result.Authenticated || !result.HeartbeatTestRequest || !result.ResendSequenceReset || !result.ReconciliationCallback || result.MockFinalOrderStatus != "4" || result.SecurityListMultiplier != "10" {
		t.Fatalf("unexpected evidence: %+v", result)
	}
	if result.TradingWritePerformed {
		t.Fatal("session-only mock reported a trading write")
	}
}

func TestDeribitFIXTestnetRequiresIndependentFIXGate(t *testing.T) {
	t.Setenv("RUN_DERIBIT_FIX_TESTS", "0")
	cfg := config.Config{Deribit: config.DeribitConfig{FIXEnabled: true}}
	handled, err := executeDeribitFIXCommand(
		context.Background(),
		cfg,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		[]string{"venue", "deribit", "fix", "connect-testnet"},
		io.Discard,
	)
	if !handled {
		t.Fatal("Deribit FIX command was not handled")
	}
	if err == nil || !strings.Contains(err.Error(), "RUN_DERIBIT_FIX_TESTS=1") {
		t.Fatalf("error=%v, want explicit FIX gate", err)
	}
}

func TestDeribitFIXRejectsUnknownAndExtraMockArguments(t *testing.T) {
	cfg := config.Config{}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, args := range [][]string{
		{"venue", "deribit", "fix", "unknown"},
		{"venue", "deribit", "fix", "mock-demo", "extra"},
	} {
		handled, err := executeDeribitFIXCommand(context.Background(), cfg, logger, args, io.Discard)
		if !handled || err == nil {
			t.Errorf("args=%v handled=%v err=%v", args, handled, err)
		}
	}
}

func TestDeribitFIXCredentialClassificationOnlyCoversLiveConnection(t *testing.T) {
	if isDeribitAuthenticatedCommand([]string{"venue", "deribit", "fix", "mock-demo"}) {
		t.Fatal("local mock incorrectly requires exchange credentials")
	}
	if !isDeribitAuthenticatedCommand([]string{"venue", "deribit", "fix", "connect-testnet"}) {
		t.Fatal("live Deribit FIX connection did not require exchange credentials")
	}
}
