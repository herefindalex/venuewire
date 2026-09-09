package orderstate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bybit/internal/domain"
)

func TestFileStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "orders.json")
	store := FileStore{Path: path}
	snapshot := NewSnapshot()
	snapshot.Orders["client-1"] = domain.Order{OrderID: "exchange-1", OrderLinkID: "client-1", Status: domain.OrderStatusPendingSubmit}
	if err := store.Save(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Orders["client-1"]; got.OrderID != "exchange-1" || got.Status != domain.OrderStatusPendingSubmit {
		t.Fatalf("unexpected order: %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o, want 600", got)
	}
}

func TestFileStoreRejectsMalformedAndUnknownVersion(t *testing.T) {
	tests := []struct{ name, body string }{
		{"malformed", "{"},
		{"version", `{"version":99}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "orders.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := (FileStore{Path: path}).Load(context.Background()); err == nil {
				t.Fatal("expected explicit load error")
			}
		})
	}
}

func TestFileStoreHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := FileStore{Path: filepath.Join(t.TempDir(), "orders.json")}
	if _, err := store.Load(ctx); err == nil {
		t.Fatal("Load accepted cancelled context")
	}
	if err := store.Save(ctx, NewSnapshot()); err == nil {
		t.Fatal("Save accepted cancelled context")
	}
}
