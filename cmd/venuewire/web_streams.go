package main

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"venuewire/internal/accountstate"
	"venuewire/internal/config"
	"venuewire/internal/deribit"
	"venuewire/internal/domain"
	"venuewire/internal/tradereconcile"
	bybitws "venuewire/internal/ws"
)

func runWebVenueStreams(ctx context.Context, cfg config.Config, webConfig config.WebConfig, accounts *accountstate.Manager, rechecker *tradereconcile.Service, deribitClient *deribit.Client, logger *slog.Logger) {
	accountRefresh := make(chan domain.Venue, 2)
	tradeRefresh := make(chan struct{}, 1)
	go runWebRefreshTriggers(ctx, accountRefresh, tradeRefresh, accounts, rechecker, logger)
	if webConfig.BybitEnabled {
		runBybitWebStreams(ctx, cfg, webConfig, accounts, accountRefresh, tradeRefresh, logger)
	}
	if webConfig.DeribitEnabled && deribitClient != nil {
		runDeribitWebStreams(ctx, cfg, webConfig, accounts, deribitClient, accountRefresh, tradeRefresh, logger)
	}
}

func runBybitWebStreams(ctx context.Context, cfg config.Config, webConfig config.WebConfig, accounts *accountstate.Manager, accountRefresh chan<- domain.Venue, tradeRefresh chan<- struct{}, logger *slog.Logger) {
	public := bybitws.NewClient(bybitws.Config{URL: cfg.WSPublicSpotURL, Topics: []string{"orderbook.50.BTCUSDT"}, StaleAfter: 15 * time.Second})
	private := bybitws.NewPrivateClient(bybitws.PrivateConfig{
		URL: cfg.WSPrivateURL, APIKey: cfg.APIKey, APISecret: cfg.APISecret,
		Topics: []string{"wallet", "order.spot", "execution.spot"}, StaleAfter: 30 * time.Second,
		OnReconnect: func(context.Context) error {
			triggerVenue(accountRefresh, domain.VenueBybit)
			triggerTrade(tradeRefresh)
			return nil
		},
	})
	var publicEventMS, privateEventMS atomic.Int64
	go logStreamExit(ctx, logger, "Bybit public Spot WebSocket", func() error {
		return public.Run(ctx, func(_ context.Context, event bybitws.Event) error {
			publicEventMS.Store(event.ReceivedAt.UnixMilli())
			accounts.UpdatePublicWS(domain.VenueBybit, "LIVE", event.ReceivedAt, public.Stats().Reconnects)
			return nil
		})
	})
	go logStreamExit(ctx, logger, "Bybit private WebSocket", func() error {
		return private.Run(ctx, func(_ context.Context, event bybitws.PrivateEvent) error {
			privateEventMS.Store(event.ReceivedAt.UnixMilli())
			reconnects, _, _ := private.Stats()
			accounts.UpdatePrivateWS(domain.VenueBybit, "LIVE", event.ReceivedAt, reconnects)
			if event.Kind == bybitws.PrivateWallet {
				triggerVenue(accountRefresh, domain.VenueBybit)
			} else if event.Kind == bybitws.PrivateOrder || event.Kind == bybitws.PrivateExecution {
				triggerTrade(tradeRefresh)
			}
			return nil
		})
	})
	go monitorBybitStreams(ctx, webConfig, accounts, public, private, &publicEventMS, &privateEventMS)
}

func runDeribitWebStreams(ctx context.Context, cfg config.Config, webConfig config.WebConfig, accounts *accountstate.Manager, httpClient *deribit.Client, accountRefresh chan<- domain.Venue, tradeRefresh chan<- struct{}, logger *slog.Logger) {
	public, err := deribit.NewWSClient(nil, deribit.WSConfig{URL: cfg.Deribit.WSURL, Channels: []string{"book.ETH_BTC.none.50.100ms"}, StaleAfter: 15 * time.Second})
	if err != nil {
		logger.Warn("Deribit public WebSocket configuration rejected")
		return
	}
	private, err := deribit.NewWSClient(httpClient, deribit.WSConfig{
		URL: cfg.Deribit.WSURL, Channels: []string{"user.portfolio.any", "user.orders.ETH_BTC.raw", "user.trades.ETH_BTC.raw"}, Private: true,
		StaleAfter: 30 * time.Second,
		OnReady: func(_ context.Context, _ uint64) error {
			triggerVenue(accountRefresh, domain.VenueDeribit)
			triggerTrade(tradeRefresh)
			return nil
		},
	})
	if err != nil {
		logger.Warn("Deribit private WebSocket configuration rejected")
		return
	}
	var publicEventMS, privateEventMS atomic.Int64
	go logStreamExit(ctx, logger, "Deribit public Spot WebSocket", func() error {
		return public.Run(ctx, func(_ context.Context, event deribit.WSNotification) error {
			publicEventMS.Store(event.ReceivedAt.UnixMilli())
			accounts.UpdatePublicWS(domain.VenueDeribit, "LIVE", event.ReceivedAt, public.Metrics().Reconnects)
			return nil
		})
	})
	go logStreamExit(ctx, logger, "Deribit private WebSocket", func() error {
		return private.Run(ctx, func(_ context.Context, event deribit.WSNotification) error {
			privateEventMS.Store(event.ReceivedAt.UnixMilli())
			accounts.UpdatePrivateWS(domain.VenueDeribit, "LIVE", event.ReceivedAt, private.Metrics().Reconnects)
			if event.Channel == "user.portfolio.any" {
				triggerVenue(accountRefresh, domain.VenueDeribit)
			} else {
				triggerTrade(tradeRefresh)
			}
			return nil
		})
	})
	go monitorDeribitStreams(ctx, webConfig, accounts, public, private, &publicEventMS, &privateEventMS)
}

func runWebRefreshTriggers(ctx context.Context, accountsIn <-chan domain.Venue, trades <-chan struct{}, accounts *accountstate.Manager, rechecker *tradereconcile.Service, logger *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case venue := <-accountsIn:
			refreshContext, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := accounts.Refresh(refreshContext, venue); err != nil {
				logger.Warn("stream-triggered account refresh incomplete", slog.String("venue", string(venue)))
			}
			cancel()
		case <-trades:
			recheckContext, cancel := context.WithTimeout(ctx, 20*time.Second)
			if err := rechecker.Recover(recheckContext, time.Now()); err != nil {
				logger.Warn("stream-triggered trade reconciliation incomplete")
			}
			cancel()
		}
	}
}

func monitorBybitStreams(ctx context.Context, webConfig config.WebConfig, accounts *accountstate.Manager, public *bybitws.Client, private *bybitws.PrivateClient, publicEventMS, privateEventMS *atomic.Int64) {
	monitorStreams(ctx, webConfig, func(now time.Time) {
		publicStats := public.Stats()
		privateReconnects, _, _ := private.Stats()
		accounts.UpdatePublicWS(domain.VenueBybit, streamState(public.Connected(), publicEventMS.Load(), now, webConfig.TradeBookMaxAge), unixTime(publicEventMS.Load()), publicStats.Reconnects)
		accounts.UpdatePrivateWS(domain.VenueBybit, streamState(private.Connected(), privateEventMS.Load(), now, 2*webConfig.AccountReconcileInterval), unixTime(privateEventMS.Load()), privateReconnects)
	})
}

func monitorDeribitStreams(ctx context.Context, webConfig config.WebConfig, accounts *accountstate.Manager, public, private *deribit.WSClient, publicEventMS, privateEventMS *atomic.Int64) {
	monitorStreams(ctx, webConfig, func(now time.Time) {
		accounts.UpdatePublicWS(domain.VenueDeribit, streamState(public.Connected(), publicEventMS.Load(), now, webConfig.TradeBookMaxAge), unixTime(publicEventMS.Load()), public.Metrics().Reconnects)
		accounts.UpdatePrivateWS(domain.VenueDeribit, streamState(private.Connected(), privateEventMS.Load(), now, 2*webConfig.AccountReconcileInterval), unixTime(privateEventMS.Load()), private.Metrics().Reconnects)
	})
}

func monitorStreams(ctx context.Context, _ config.WebConfig, update func(time.Time)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			update(now)
		}
	}
}

func streamState(connected bool, eventMS int64, now time.Time, staleAfter time.Duration) string {
	if eventMS == 0 {
		return "CONNECTING"
	}
	if !connected {
		return "RECONNECTING"
	}
	if now.Sub(time.UnixMilli(eventMS)) > staleAfter {
		return "STALE"
	}
	return "LIVE"
}

func unixTime(milliseconds int64) time.Time {
	if milliseconds == 0 {
		return time.Time{}
	}
	return time.UnixMilli(milliseconds).UTC()
}

func triggerVenue(target chan<- domain.Venue, venue domain.Venue) {
	select {
	case target <- venue:
	default:
	}
}
func triggerTrade(target chan<- struct{}) {
	select {
	case target <- struct{}{}:
	default:
	}
}

func logStreamExit(ctx context.Context, logger *slog.Logger, name string, run func() error) {
	if err := run(); err != nil && ctx.Err() == nil {
		logger.Warn("venue stream stopped", slog.String("stream", name))
	}
}
