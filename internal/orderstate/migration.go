package orderstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bybit/internal/domain"
)

type MigrationOptions struct {
	BybitAccountAlias string
	DryRun            bool
	BackupPath        string
}

type MigrationReport struct {
	FromVersion int
	ToVersion   int
	Orders      int
	Executions  int
	BackupPath  string
	DryRun      bool
	Changed     bool
}

type snapshotV1 struct {
	Version    int                         `json:"version"`
	UpdatedAt  time.Time                   `json:"updatedAt"`
	Orders     map[string]domain.Order     `json:"orders"`
	Executions map[string]domain.Execution `json:"executions"`
}

// MigrateV1ToV2 performs an explicit, fail-closed migration. Applying a
// migration first preserves the exact v1 bytes in a mode-0600 backup.
func MigrateV1ToV2(ctx context.Context, store FileStore, options MigrationOptions) (MigrationReport, error) {
	report := MigrationReport{FromVersion: 1, ToVersion: SnapshotVersion, DryRun: options.DryRun}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if strings.TrimSpace(options.BybitAccountAlias) == "" {
		return report, errors.New("explicit Bybit account alias is required")
	}
	raw, err := os.ReadFile(store.Path)
	if err != nil {
		return report, fmt.Errorf("read state for migration: %w", err)
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return report, fmt.Errorf("decode state header: %w", err)
	}
	if header.Version == SnapshotVersion {
		report.FromVersion, report.Changed = SnapshotVersion, false
		return report, nil
	}
	if header.Version != 1 {
		return report, fmt.Errorf("unsupported source snapshot version %d", header.Version)
	}
	var old snapshotV1
	if err := json.Unmarshal(raw, &old); err != nil {
		return report, fmt.Errorf("decode v1 state: %w", err)
	}

	account := domain.AccountKey{Venue: domain.VenueBybit, Environment: domain.EnvironmentTestnet, Alias: options.BybitAccountAlias}
	if err := account.Validate(); err != nil {
		return report, fmt.Errorf("migration account mapping: %w", err)
	}
	orders := make(map[string]domain.Order, len(old.Orders))
	orderCategories := make(map[string]string)
	for legacyKey, order := range old.Orders {
		if strings.TrimSpace(order.Category) == "" {
			return report, fmt.Errorf("order %q has ambiguous category", legacyKey)
		}
		order.Exchange, order.Environment, order.AccountAlias = string(account.Venue), string(account.Environment), account.Alias
		key := legacyKey
		if account.Alias != "bybit-test" {
			key = orderStorageKey(order)
		}
		if key == "" {
			return report, fmt.Errorf("order %q has no stable identifier", legacyKey)
		}
		if _, exists := orders[key]; exists {
			return report, fmt.Errorf("order key collision after migration: %q", key)
		}
		orders[key] = order
		if order.OrderID != "" {
			orderCategories[order.OrderID] = order.Category
		}
	}
	executions := make(map[string]domain.Execution, len(old.Executions))
	for legacyKey, execution := range old.Executions {
		if strings.TrimSpace(execution.Category) == "" {
			execution.Category = orderCategories[execution.OrderID]
		}
		if strings.TrimSpace(execution.Category) == "" {
			return report, fmt.Errorf("execution %q has ambiguous category", legacyKey)
		}
		execution.Exchange, execution.Environment, execution.AccountAlias = string(account.Venue), string(account.Environment), account.Alias
		key := legacyKey
		if account.Alias != "bybit-test" {
			key = executionStorageKey(execution)
		}
		if key == "" {
			return report, fmt.Errorf("execution %q has no stable identifier", legacyKey)
		}
		if _, exists := executions[key]; exists {
			return report, fmt.Errorf("execution key collision after migration: %q", key)
		}
		executions[key] = execution
	}
	next := &Snapshot{Version: SnapshotVersion, UpdatedAt: old.UpdatedAt, Orders: orders, Executions: executions}
	report.Orders, report.Executions, report.Changed = len(orders), len(executions), true
	if options.DryRun {
		return report, nil
	}
	backup := options.BackupPath
	if backup == "" {
		backup = store.Path + ".v1.bak"
	}
	report.BackupPath = backup
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		return report, fmt.Errorf("create backup directory: %w", err)
	}
	file, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return report, fmt.Errorf("create migration backup: %w", err)
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return report, fmt.Errorf("write migration backup: %w", err)
	}
	if err := store.Save(ctx, next); err != nil {
		return report, fmt.Errorf("save v2 state (v1 backup retained at %s): %w", backup, err)
	}
	return report, nil
}

// RestoreV1Backup atomically restores exact backed-up bytes after validating
// that they are a v1 snapshot. The backup itself is retained.
func RestoreV1Backup(ctx context.Context, store FileStore, backupPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if backupPath == "" {
		backupPath = store.Path + ".v1.bak"
	}
	raw, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read migration backup: %w", err)
	}
	var old snapshotV1
	if err := json.Unmarshal(raw, &old); err != nil || old.Version != 1 {
		return errors.New("backup is not a valid v1 snapshot")
	}
	if err := os.MkdirAll(filepath.Dir(store.Path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(store.Path), ".restore-v1-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err = temp.Chmod(0o600); err == nil {
		_, err = temp.Write(raw)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write restored state: %w", err)
	}
	if err := os.Rename(tempName, store.Path); err != nil {
		return fmt.Errorf("replace state with backup: %w", err)
	}
	return nil
}
