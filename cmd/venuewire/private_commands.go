package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/herefindalex/venuewire/internal/config"
	"github.com/herefindalex/venuewire/internal/orderstate"
	"github.com/herefindalex/venuewire/internal/reconcile"
	"github.com/herefindalex/venuewire/internal/rest"
	bybitws "github.com/herefindalex/venuewire/internal/ws"
)

func executePrivateStreamCommand(ctx context.Context, cfg config.Config, logger *slog.Logger, args []string, output io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "private-stream" {
		return false, nil
	}
	flags := newFlagSet("private-stream")
	symbol := flags.String("symbol", "BTCUSDT", "symbol used for REST reconciliation")
	if err := flags.Parse(args[1:]); err != nil {
		return true, err
	}
	state, err := orderstate.NewService(ctx, orderstate.FileStore{Path: cfg.StateFile})
	if err != nil {
		return true, fmt.Errorf("load local order state: %w", err)
	}
	reconciler := reconcile.Reconciler{State: state, Exchange: reconcile.RESTReader{Client: rest.NewClient(cfg.RESTBaseURL, cfg.APIKey, cfg.APISecret), Category: "linear", Symbols: []string{*symbol}}}
	startupReport, err := reconciler.Reconcile(ctx)
	if err != nil {
		return true, fmt.Errorf("startup reconciliation: %w", err)
	}
	logger.Info("startup reconciliation complete", slog.Int("discrepancies", len(startupReport.Discrepancies)))
	client := bybitws.NewPrivateClient(bybitws.PrivateConfig{
		URL: cfg.WSPrivateURL, APIKey: cfg.APIKey, APISecret: cfg.APISecret,
		OnReconnect: func(reconnectContext context.Context) error {
			report, reconcileErr := reconciler.Reconcile(reconnectContext)
			if reconcileErr == nil {
				logger.Warn("private WebSocket reconnected and REST reconciliation completed", slog.String("protocol", "WS"), slog.Int("discrepancies", len(report.Discrepancies)))
			}
			return reconcileErr
		},
	})
	logger.Info("private WebSocket stream starting", slog.String("protocol", "WS"))
	err = client.Run(ctx, func(eventContext context.Context, event bybitws.PrivateEvent) error {
		switch event.Kind {
		case bybitws.PrivateOrder:
			if err := state.ApplyOrder(eventContext, *event.Order); err != nil {
				return fmt.Errorf("apply private order event %s: %w", event.EventID, err)
			}
		case bybitws.PrivateExecution:
			if err := state.ApplyExecution(eventContext, *event.Execution); err != nil {
				return fmt.Errorf("apply private execution event %s: %w", event.EventID, err)
			}
		}
		logger.Info("private WebSocket event", slog.String("protocol", "WS"), slog.String("topic", event.Topic), slog.String("event_id", event.EventID), slog.Time("exchange_timestamp", event.ExchangeTime), slog.Time("receive_timestamp", event.ReceivedAt))
		return writeJSON(output, event)
	})
	reconnects, messages, queueDepth := client.Stats()
	logger.Info("private WebSocket stream stopped", slog.String("protocol", "WS"), slog.Uint64("messages", messages), slog.Uint64("reconnects", reconnects), slog.Int64("queue_depth", queueDepth))
	return true, err
}
