package intent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTripPermissionsAndDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "intents.json")
	store := Store{Path: path}
	plan := Plan{ID: "one", Status: StatusPlanned, ExpiresAt: time.Now().Add(time.Minute)}
	if err := store.SavePlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(context.Background(), plan); err == nil {
		t.Fatal("duplicate accepted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	loaded, err := store.Get(context.Background(), "one")
	if err != nil || loaded.ID != "one" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestExpiredClaimPersistsTerminalStatus(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "intents.json")}
	plan := Plan{ID: "expired", Status: StatusPlanned, ExpiresAt: time.Now().Add(-time.Second)}
	if err := store.SavePlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(context.Background(), plan.ID, time.Now()); err == nil {
		t.Fatal("expired plan claimed")
	}
	got, err := store.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusExpired {
		t.Fatalf("status=%s", got.Status)
	}
}
