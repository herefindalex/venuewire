package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"bybit/internal/config"
	"bybit/internal/orderstate"
)

func executeStateCommand(ctx context.Context, cfg config.Config, args []string, output io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "state" {
		return false, nil
	}
	if len(args) < 2 {
		return true, errors.New("state requires migrate-v1 or restore-v1")
	}
	store := orderstate.FileStore{Path: cfg.StateFile}
	switch args[1] {
	case "migrate-v1":
		flags := flag.NewFlagSet("state migrate-v1", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		alias := flags.String("bybit-account-alias", "", "explicit account mapping")
		dryRun := flags.Bool("dry-run", false, "validate without writing")
		backup := flags.String("backup", "", "backup path")
		if err := flags.Parse(args[2:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 {
			return true, fmt.Errorf("unexpected state migration arguments: %v", flags.Args())
		}
		report, err := orderstate.MigrateV1ToV2(ctx, store, orderstate.MigrationOptions{
			BybitAccountAlias: *alias, DryRun: *dryRun, BackupPath: *backup,
		})
		if err != nil {
			return true, err
		}
		return true, json.NewEncoder(output).Encode(report)
	case "restore-v1":
		flags := flag.NewFlagSet("state restore-v1", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		backup := flags.String("backup", "", "backup path")
		if err := flags.Parse(args[2:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 {
			return true, fmt.Errorf("unexpected state restore arguments: %v", flags.Args())
		}
		if err := orderstate.RestoreV1Backup(ctx, store, *backup); err != nil {
			return true, err
		}
		_, err := fmt.Fprintln(output, `{"restoredVersion":1}`)
		return true, err
	default:
		return true, fmt.Errorf("unknown state command %q", args[1])
	}
}
