package orderstate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bybit/internal/domain"
)

func writeV1(t *testing.T, path string, snapshot snapshotV1) []byte {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMigrationDryRunDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	original := writeV1(t, path, snapshotV1{Version: 1, Orders: map[string]domain.Order{
		"link": {Category: "linear", OrderID: "order", OrderLinkID: "link"},
	}, Executions: map[string]domain.Execution{}})
	report, err := MigrateV1ToV2(context.Background(), FileStore{Path: path}, MigrationOptions{BybitAccountAlias: "bybit-test", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Changed || !report.DryRun || report.Orders != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatal("dry-run changed state bytes")
	}
	if _, err := os.Stat(path + ".v1.bak"); !os.IsNotExist(err) {
		t.Fatal("dry-run created backup")
	}
}

func TestMigrationBacksUpCanRestoreAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	original := writeV1(t, path, snapshotV1{Version: 1, Orders: map[string]domain.Order{
		"link": {Category: "linear", OrderID: "order", OrderLinkID: "link"},
	}, Executions: map[string]domain.Execution{
		"trade": {ExecutionID: "trade", OrderID: "order"},
	}})
	store := FileStore{Path: path}
	report, err := MigrateV1ToV2(context.Background(), store, MigrationOptions{BybitAccountAlias: "bybit-test"})
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(report.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(original) {
		t.Fatal("backup is not byte-for-byte v1 state")
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != 2 || loaded.Orders["link"].AccountAlias != "bybit-test" || loaded.Executions["trade"].Category != "linear" {
		t.Fatalf("unexpected migrated snapshot: %+v", loaded)
	}
	second, err := MigrateV1ToV2(context.Background(), store, MigrationOptions{BybitAccountAlias: "bybit-test"})
	if err != nil || second.Changed {
		t.Fatalf("rerun not idempotent: %+v %v", second, err)
	}
	if err := RestoreV1Backup(context.Background(), store, report.BackupPath); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Fatal("restore did not reproduce original bytes")
	}
}

func TestMigrationRefusesAmbiguousCategoryAndMissingMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	writeV1(t, path, snapshotV1{Version: 1, Orders: map[string]domain.Order{"link": {OrderLinkID: "link"}}})
	for _, options := range []MigrationOptions{{BybitAccountAlias: "bybit-test"}, {}} {
		if _, err := MigrateV1ToV2(context.Background(), FileStore{Path: path}, options); err == nil {
			t.Fatalf("unsafe migration accepted with options %+v", options)
		}
	}
}

func TestMigrationUsesCompoundKeysForNonDefaultAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.json")
	writeV1(t, path, snapshotV1{Version: 1, Orders: map[string]domain.Order{
		"link": {Category: "linear", OrderID: "order", OrderLinkID: "link"},
	}})
	store := FileStore{Path: path}
	if _, err := MigrateV1ToV2(context.Background(), store, MigrationOptions{BybitAccountAlias: "desk-a"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, legacy := loaded.Orders["link"]; legacy {
		t.Fatal("non-default account retained collision-prone legacy key")
	}
	if len(loaded.Orders) != 1 {
		t.Fatalf("unexpected order count: %d", len(loaded.Orders))
	}
}
