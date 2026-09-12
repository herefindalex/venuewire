package main

import (
	"context"
	"io"
	"log/slog"

	"github.com/herefindalex/venuewire/internal/config"
	"github.com/herefindalex/venuewire/internal/orderstate"
	"github.com/herefindalex/venuewire/internal/reconcile"
	"github.com/herefindalex/venuewire/internal/rest"
)

func executeReconcileCommand(ctx context.Context, cfg config.Config, logger *slog.Logger, args []string, output io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "reconcile" {
		return false, nil
	}
	flags := newFlagSet("reconcile")
	category := flags.String("category", "linear", "product category")
	symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
	if err := flags.Parse(args[1:]); err != nil {
		return true, err
	}
	state, err := orderstate.NewService(ctx, orderstate.FileStore{Path: cfg.StateFile})
	if err != nil {
		return true, err
	}
	engine := reconcile.Reconciler{State: state, Exchange: reconcile.RESTReader{Client: rest.NewClient(cfg.RESTBaseURL, cfg.APIKey, cfg.APISecret), Category: *category, Symbols: []string{*symbol}}}
	report, err := engine.Reconcile(ctx)
	if err != nil {
		return true, err
	}
	logger.Info("reconciliation complete", slog.String("protocol", "REST"), slog.Int("open_orders", report.OpenFetched), slog.Int("recent_orders", report.RecentFetched), slog.Int("executions", report.ExecFetched), slog.Int("discrepancies", len(report.Discrepancies)))
	return true, writeJSON(output, report)
}
