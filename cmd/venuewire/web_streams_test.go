package main

import (
	"testing"
	"time"
)

func TestStreamState(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Second).UnixMilli()
	old := now.Add(-10 * time.Second).UnixMilli()
	tests := []struct {
		name       string
		connected  bool
		eventMS    int64
		staleAfter time.Duration
		want       string
	}{
		{name: "initial connection", connected: false, staleAfter: 3 * time.Second, want: "CONNECTING"},
		{name: "live", connected: true, eventMS: recent, staleAfter: 3 * time.Second, want: "LIVE"},
		{name: "stale", connected: true, eventMS: old, staleAfter: 3 * time.Second, want: "STALE"},
		{name: "reconnecting", connected: false, eventMS: recent, staleAfter: 3 * time.Second, want: "RECONNECTING"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := streamState(test.connected, test.eventMS, now, test.staleAfter); got != test.want {
				t.Fatalf("streamState() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestUnixTimeZeroAndUTC(t *testing.T) {
	if got := unixTime(0); !got.IsZero() {
		t.Fatalf("unixTime(0) = %v", got)
	}
	got := unixTime(1700000000000)
	if got.Location() != time.UTC || got.UnixMilli() != 1700000000000 {
		t.Fatalf("unixTime() = %v", got)
	}
}
