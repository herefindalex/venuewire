package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"bybit/internal/config"
)

func TestBybitFIXMockDemoTreatsCompletedCancellationAsClean(t *testing.T) {
	for iteration := 0; iteration < 5; iteration++ {
		cfg := config.Config{StateFile: filepath.Join(t.TempDir(), "orders.json")}
		var output bytes.Buffer
		err := runFIXMockDemo(
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
}
