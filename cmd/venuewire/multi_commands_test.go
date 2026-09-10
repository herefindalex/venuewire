package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"venuewire/internal/config"
)

func TestNormalizeVenueArgsPreservesLegacyBybitAndAddsSharedRouting(t *testing.T) {
	tests := []struct{ input, want []string }{{[]string{"--venue", "bybit", "time"}, []string{"time"}}, {[]string{"--venue", "deribit", "doctor"}, []string{"venue", "deribit", "doctor"}}, {[]string{"--venue", "all", "status"}, []string{"venue", "all", "status"}}, {[]string{"time"}, []string{"time"}}}
	for _, tt := range tests {
		got := normalizeVenueArgs(tt.input)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Fatalf("normalize %v=%v want %v", tt.input, got, tt.want)
		}
	}
}

func TestAllVenueStatusReportsFailuresIndependently(t *testing.T) {
	cfg := config.Config{Environment: "testnet", AccountAlias: "bybit-test", Deribit: config.DeribitConfig{Environment: "testnet", AccountAlias: "deribit-test"}}
	var output bytes.Buffer
	handled, err := executeMultiVenueCommand(context.Background(), cfg, []string{"venue", "all", "status"}, &output)
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	var result struct {
		Results    []venueResult `json:"results"`
		AllHealthy bool          `json:"allHealthy"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.AllHealthy || len(result.Results) != 2 {
		t.Fatalf("result=%+v", result)
	}
	if result.Results[0].Error == "" || result.Results[1].Error == "" {
		t.Fatalf("venue errors not isolated: %+v", result.Results)
	}
}
