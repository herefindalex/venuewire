package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/herefindalex/venuewire/internal/accountstate"
	"github.com/herefindalex/venuewire/internal/config"
	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/intent"
	"github.com/herefindalex/venuewire/internal/quicktrade"
	"github.com/herefindalex/venuewire/internal/rest"
	"github.com/herefindalex/venuewire/internal/runtimeevent"
	"github.com/herefindalex/venuewire/internal/spotadapter"
	"github.com/herefindalex/venuewire/internal/tradereconcile"
	"github.com/herefindalex/venuewire/internal/webassets"
	"github.com/herefindalex/venuewire/internal/webconsole"
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
	if err := accounts.SetValuationMaxAge(webConfig.ValuationPriceMaxAge); err != nil {
		return true, fmt.Errorf("configure account valuation: %w", err)
	}
	events := runtimeevent.NewBroker()
	accounts.OnEvent = func(eventType string, venue domain.Venue, at time.Time) {
		events.Publish(runtimeevent.Event{Type: eventType, Venue: venue, At: at})
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
				ByAsset: map[string]string{"BTC": webConfig.QuickTradeMaxBTC, "ETH": webConfig.QuickTradeMaxETH, "USDT": webConfig.QuickTradeMaxUSDT, "USDC": webConfig.QuickTradeMaxUSDC},
				ByVenueSource: map[string]string{
					"bybit:BTC": webConfig.MaxBybitBTCQty, "bybit:ETH": webConfig.MaxBybitETHQty, "bybit:USDT": webConfig.MaxBybitUSDTAmount,
					"deribit:BTC": webConfig.MaxDeribitBTCAmount, "deribit:USDC": webConfig.MaxDeribitUSDCAmount,
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
	tradeApplication.OnSubmission = func(observation quicktrade.SubmissionObservation) {
		recordSubmissionObservation(accounts, observation)
	}
	rechecker := &tradereconcile.Service{Store: tradeStore, Accounts: accounts}
	rechecker.OnEvent = func(eventType string, venue domain.Venue, intentID string, at time.Time) {
		events.Publish(runtimeevent.Event{Type: eventType, Venue: venue, IntentID: intentID, At: at})
	}
	if bybitClient != nil {
		rechecker.Bybit = bybitClient
	}
	if deribitClient != nil {
		rechecker.Deribit = deribitClient
	}
	runWebVenueStreams(accountContext, cfg, webConfig, accounts, rechecker, deribitClient, logger)
	go runQuickTradeRecovery(accountContext, webConfig.AccountReconcileInterval, rechecker, logger)
	logger.Info("web console starting", slog.String("listen", webConfig.ListenAddress()), slog.String("environment", "testnet"), slog.Bool("tradingEnabled", webConfig.TradingEnabled))
	return true, webconsole.New(webConfig, cfg, logger,
		webconsole.WithAssets(assets),
		webconsole.WithAccountService(accounts),
		webconsole.WithTradeApplication(tradeApplication),
		webconsole.WithTradeRechecker(rechecker),
		webconsole.WithEventBroker(events),
	).ListenAndServe(ctx)
}

func recordSubmissionObservation(accounts *accountstate.Manager, observation quicktrade.SubmissionObservation) {
	if accounts == nil {
		return
	}
	accounts.RecordOrderSubmission(
		observation.Venue,
		observation.ClientOrderID,
		observation.VenueOrderID,
		observation.AckAt,
		observation.RequestRTT,
		observation.Err == nil && observation.VenueOrderID != "",
		submissionRateLimited(observation.Err),
	)
}

func submissionRateLimited(err error) bool {
	var bybitError *rest.APIError
	if errors.As(err, &bybitError) && (bybitError.HTTPStatus == 429 || bybitError.Code == 10006) {
		return true
	}
	var deribitError *deribit.RPCError
	return errors.As(err, &deribitError) && deribitError.Code == 10028
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
