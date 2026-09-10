package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"venuewire/internal/config"
	"venuewire/internal/webassets"
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
	assets, ok := webassets.FS()
	if !ok {
		return true, errors.New("web UI is not embedded; build with make build or go build -tags webui after building web assets")
	}
	logger.Info("web console starting", slog.String("listen", webConfig.ListenAddress()), slog.String("environment", "testnet"), slog.Bool("tradingEnabled", webConfig.TradingEnabled))
	return true, webconsole.New(webConfig, cfg, logger, webconsole.WithAssets(assets)).ListenAndServe(ctx)
}
