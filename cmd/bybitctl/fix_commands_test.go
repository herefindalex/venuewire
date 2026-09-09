package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"bybit/internal/config"
	"bybit/internal/orderstate"
)

func TestBybitFIXMockDemoTreatsCompletedCancellationAsClean(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "orders.json")
	store := orderstate.FileStore{Path: statePath}
	if err := store.Save(context.Background(), orderstate.NewSnapshot()); err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	for iteration := 0; iteration < 5; iteration++ {
		cfg := config.Config{StateFile: statePath}
		var output bytes.Buffer
		err = runFIXMockDemo(
			context.Background(),
			cfg,
			slog.New(slog.NewJSONHandler(io.Discard, nil)),
			&output,
		)
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		var snapshot struct {
			Orders map[string]any `json:"orders"`
		}
		if err := json.Unmarshal(output.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Orders) != 1 {
			t.Fatalf("iteration %d orders=%d", iteration, len(snapshot.Orders))
		}
	}

	stateAfter, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateAfter, stateBefore) {
		t.Fatal("local FIX mock modified the configured persistent order state")
	}
}
