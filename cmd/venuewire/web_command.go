package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"venuewire/internal/accountstate"
	"venuewire/internal/config"
	"venuewire/internal/deribit"
	"venuewire/internal/rest"
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
	providers := make([]accountstate.Provider, 0, 2)
	if webConfig.BybitEnabled {
		providers = append(providers, &accountstate.BybitProvider{
			Client:       rest.NewClient(cfg.RESTBaseURL, cfg.APIKey, cfg.APISecret),
			AccountAlias: cfg.AccountAlias,
		})
	}
	if webConfig.DeribitEnabled {
		client, clientErr := deribit.NewClient(cfg.Deribit.HTTPBaseURL, cfg.Deribit.APIKey, cfg.Deribit.APISecret, nil)
		if clientErr != nil {
			return true, fmt.Errorf("create Deribit account client: %w", clientErr)
		}
		providers = append(providers, &accountstate.DeribitProvider{Client: client, AccountAlias: cfg.Deribit.AccountAlias})
	}
	accounts, err := accountstate.NewManager(providers, webConfig.AccountReconcileInterval, 2*webConfig.AccountReconcileInterval)
	if err != nil {
		return true, fmt.Errorf("create account manager: %w", err)
	}
	accountContext, stopAccounts := context.WithCancel(ctx)
	defer stopAccounts()
	go accounts.Run(accountContext)
	logger.Info("web console starting", slog.String("listen", webConfig.ListenAddress()), slog.String("environment", "testnet"), slog.Bool("tradingEnabled", webConfig.TradingEnabled))
	return true, webconsole.New(webConfig, cfg, logger,
		webconsole.WithAssets(assets),
		webconsole.WithAccountService(accounts),
	).ListenAndServe(ctx)
}
