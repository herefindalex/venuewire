package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"venuewire/internal/config"
	"venuewire/internal/webconsole"
)

func executeWebCommand(ctx context.Context, cfg config.Config, logger *slog.Logger, args []string) (bool, error) {
	if len(args) == 0 || args[0] != "web" {
		return false, nil
	}
	if len(args) != 1 {
		return true, errors.New("usage: venuewire [--env-file PATH] web")
	}
	webConfig, err := config.LoadWebConfig(cfg)
	if err != nil {
		return true, fmt.Errorf("web configuration rejected: %w", err)
	}
	logger.Info("web console starting", slog.String("listen", webConfig.ListenAddress()), slog.String("environment", "testnet"), slog.Bool("tradingEnabled", webConfig.TradingEnabled))
	return true, webconsole.New(webConfig, cfg, logger).ListenAndServe(ctx)
}
