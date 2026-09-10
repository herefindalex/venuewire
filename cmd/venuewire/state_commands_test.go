package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"venuewire/internal/config"
)

func TestStateMigrationCommandRequiresExplicitMappingAndSupportsDryRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	original := `{"version":1,"orders":{"link":{"category":"linear","orderId":"order","orderLinkId":"link"}},"executions":{}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{StateFile: path}

	if handled, err := executeStateCommand(context.Background(), cfg, []string{"state", "migrate-v1", "--dry-run"}, &bytes.Buffer{}); !handled || err == nil {
		t.Fatalf("missing mapping was not refused: handled=%v err=%v", handled, err)
	}
	var output bytes.Buffer
	handled, err := executeStateCommand(context.Background(), cfg, []string{"state", "migrate-v1", "--bybit-account-alias", "bybit-test", "--dry-run"}, &output)
	if !handled || err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if !strings.Contains(output.String(), `"DryRun":true`) {
		t.Fatalf("unexpected report: %s", output.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != original {
		t.Fatal("command dry-run modified state")
	}
}
