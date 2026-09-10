package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"venuewire/internal/accountstate"
	"venuewire/internal/config"
	"venuewire/internal/deribit"
	"venuewire/internal/domain"
	"venuewire/internal/intent"
	"venuewire/internal/quicktrade"
	"venuewire/internal/rest"
	"venuewire/internal/spotadapter"
	"venuewire/internal/tradereconcile"
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
	var bybitClient *rest.Client
	var deribitClient *deribit.Client
	if webConfig.BybitEnabled {
		bybitClient = rest.NewClient(cfg.RESTBaseURL, cfg.APIKey, cfg.APISecret)
		providers = append(providers, &accountstate.BybitProvider{
			Client:       bybitClient,
			AccountAlias: cfg.AccountAlias,
		})
	}
	if webConfig.DeribitEnabled {
		client, clientErr := deribit.NewClient(cfg.Deribit.HTTPBaseURL, cfg.Deribit.APIKey, cfg.Deribit.APISecret, nil)
		if clientErr != nil {
			return true, fmt.Errorf("create Deribit account client: %w", clientErr)
		}
		deribitClient = client
		providers = append(providers, &accountstate.DeribitProvider{Client: deribitClient, AccountAlias: cfg.Deribit.AccountAlias})
	}
	accounts, err := accountstate.NewManager(providers, webConfig.AccountReconcileInterval, 2*webConfig.AccountReconcileInterval)
	if err != nil {
		return true, fmt.Errorf("create account manager: %w", err)
	}
	accountContext, stopAccounts := context.WithCancel(ctx)
	defer stopAccounts()
	go accounts.Run(accountContext)

	quoteProviders := make(map[domain.Venue]quicktrade.Provider, 2)
	submitters := make(map[domain.Venue]quicktrade.Submitter, 2)
	if bybitClient != nil {
		adapter := &spotadapter.Bybit{Client: bybitClient, Accounts: accounts}
		quoteProviders[domain.VenueBybit], submitters[domain.VenueBybit] = adapter, adapter
	}
	if deribitClient != nil {
		adapter := &spotadapter.Deribit{Client: deribitClient, Accounts: accounts}
		quoteProviders[domain.VenueDeribit], submitters[domain.VenueDeribit] = adapter, adapter
	}
	tradeStore := intent.Store{Path: cfg.IntentFile}
	tradeApplication := &quicktrade.Application{
		Quotes: &quicktrade.Service{
			Providers: quoteProviders, TTL: webConfig.QuoteTTL, BookMaxAge: webConfig.TradeBookMaxAge,
			SlippageBPS: webConfig.QuickTradeSlippageBPS,
			Caps: quicktrade.Caps{
				ByAsset: map[string]string{"BTC": webConfig.QuickTradeMaxBTC, "ETH": webConfig.QuickTradeMaxETH, "USDT": webConfig.QuickTradeMaxUSDT},
				ByVenueSource: map[string]string{
					"bybit:BTC": webConfig.MaxBybitBTCQty, "bybit:USDT": webConfig.MaxBybitUSDTAmount,
					"deribit:BTC": webConfig.MaxDeribitBTCAmount, "deribit:ETH": webConfig.MaxDeribitETHAmount,
				},
			},
		},
		Cache:      quicktrade.NewQuoteCache(1_000),
		Store:      tradeStore,
		Submitters: submitters,
		Limits: intent.DemoLimits{
			MaxTradesPerSession: webConfig.MaxTradesPerSession,
			MaxTradesPerHour:    webConfig.MaxTradesPerHour,
			MaxConcurrentTrades: webConfig.MaxConcurrentTrades,
		},
	}
	rechecker := &tradereconcile.Service{Store: tradeStore, Accounts: accounts}
	if bybitClient != nil {
		rechecker.Bybit = bybitClient
	}
	if deribitClient != nil {
		rechecker.Deribit = deribitClient
	}
	go runQuickTradeRecovery(accountContext, webConfig.AccountReconcileInterval, rechecker, logger)
	logger.Info("web console starting", slog.String("listen", webConfig.ListenAddress()), slog.String("environment", "testnet"), slog.Bool("tradingEnabled", webConfig.TradingEnabled))
	return true, webconsole.New(webConfig, cfg, logger,
		webconsole.WithAssets(assets),
		webconsole.WithAccountService(accounts),
		webconsole.WithTradeApplication(tradeApplication),
		webconsole.WithTradeRechecker(rechecker),
	).ListenAndServe(ctx)
}

func runQuickTradeRecovery(ctx context.Context, interval time.Duration, rechecker *tradereconcile.Service, logger *slog.Logger) {
	run := func() {
		recoveryContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := rechecker.Recover(recoveryContext, time.Now()); err != nil {
			logger.Warn("quick trade reconciliation incomplete", slog.String("error", err.Error()))
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
