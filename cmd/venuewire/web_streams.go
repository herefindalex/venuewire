package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/herefindalex/venuewire/internal/accountstate"
	"github.com/herefindalex/venuewire/internal/config"
	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/tradereconcile"
	bybitws "github.com/herefindalex/venuewire/internal/ws"
)

func runWebVenueStreams(ctx context.Context, cfg config.Config, webConfig config.WebConfig, accounts *accountstate.Manager, rechecker *tradereconcile.Service, deribitClient *deribit.Client, logger *slog.Logger) {
	accountRefresh := make(chan domain.Venue, 2)
	tradeRefresh := make(chan struct{}, 1)
	valuationUpdates := newValuationQueue()
	go runWebRefreshTriggers(ctx, accountRefresh, tradeRefresh, accounts, rechecker, logger)
	go runWebValuationUpdates(ctx, webConfig.PushInterval, accounts, valuationUpdates, logger)
	if webConfig.BybitEnabled {
		runBybitWebStreams(ctx, cfg, webConfig, accounts, accountRefresh, tradeRefresh, logger)
	}
	if webConfig.DeribitEnabled && deribitClient != nil {
		runDeribitWebStreams(ctx, cfg, webConfig, accounts, deribitClient, accountRefresh, tradeRefresh, valuationUpdates, logger)
	}
}

func runBybitWebStreams(ctx context.Context, cfg config.Config, webConfig config.WebConfig, accounts *accountstate.Manager, accountRefresh chan<- domain.Venue, tradeRefresh chan<- struct{}, logger *slog.Logger) {
	public := bybitws.NewClient(bybitws.Config{URL: cfg.WSPublicSpotURL, Topics: []string{"orderbook.50.BTCUSDT", "orderbook.50.ETHUSDT"}, StaleAfter: 15 * time.Second})
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
			} else if event.Kind == bybitws.PrivateOrder {
				if event.Order != nil {
					accounts.RecordOrderEvent(domain.VenueBybit, event.Order.OrderLinkID, event.Order.OrderID, "order", event.ReceivedAt)
				}
				triggerTrade(tradeRefresh)
			} else if event.Kind == bybitws.PrivateExecution {
				if event.Execution != nil {
					accounts.RecordOrderEvent(domain.VenueBybit, event.Execution.OrderLinkID, event.Execution.OrderID, "execution", event.ReceivedAt)
				}
				triggerTrade(tradeRefresh)
			}
			return nil
		})
	})
	go monitorBybitStreams(ctx, webConfig, accounts, public, private, &publicEventMS, &privateEventMS)
}

func runDeribitWebStreams(ctx context.Context, cfg config.Config, webConfig config.WebConfig, accounts *accountstate.Manager, httpClient *deribit.Client, accountRefresh chan<- domain.Venue, tradeRefresh chan<- struct{}, valuationUpdates *valuationQueue, logger *slog.Logger) {
	public, err := deribit.NewWSClient(nil, deribit.WSConfig{URL: cfg.Deribit.WSURL, Channels: []string{"book.BTC_USDC.none.50.100ms", "deribit_price_index.btc_usd", "deribit_price_index.eth_usd"}, StaleAfter: 15 * time.Second})
	if err != nil {
		logger.Warn("Deribit public WebSocket configuration rejected")
		return
	}
	private, err := deribit.NewWSClient(httpClient, deribit.WSConfig{
		URL: cfg.Deribit.WSURL, Channels: []string{"user.portfolio.any", "user.orders.BTC_USDC.raw", "user.trades.BTC_USDC.raw"}, Private: true,
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
			price, relevant, err := decodeDeribitUSDPrice(event)
			if err != nil {
				return err
			}
			if relevant {
				enqueueValuation(valuationUpdates, price)
			}
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
				if err := recordDeribitOrderMetric(accounts, event); err != nil {
					return err
				}
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
		privateReceiveAt, _ := private.StreamTimes()
		accounts.UpdateWSReceiveTimes(domain.VenueBybit, publicStats.LastReceiveAt, privateReceiveAt)
		accounts.UpdatePublicWS(domain.VenueBybit, streamState(public.Connected(), publicEventMS.Load(), now, webConfig.TradeBookMaxAge), unixTime(publicEventMS.Load()), publicStats.Reconnects)
		accounts.UpdatePrivateWS(domain.VenueBybit, streamState(private.Connected(), unixMilliseconds(privateReceiveAt), now, 2*webConfig.AccountReconcileInterval), unixTime(privateEventMS.Load()), privateReconnects)
	})
}

func monitorDeribitStreams(ctx context.Context, webConfig config.WebConfig, accounts *accountstate.Manager, public, private *deribit.WSClient, publicEventMS, privateEventMS *atomic.Int64) {
	monitorStreams(ctx, webConfig, func(now time.Time) {
		publicMetrics := public.Metrics()
		privateMetrics := private.Metrics()
		accounts.UpdateWSReceiveTimes(domain.VenueDeribit, publicMetrics.LastReceiveAt, privateMetrics.LastReceiveAt)
		accounts.UpdatePublicWS(domain.VenueDeribit, streamState(public.Connected(), publicEventMS.Load(), now, webConfig.TradeBookMaxAge), unixTime(publicEventMS.Load()), publicMetrics.Reconnects)
		accounts.UpdatePrivateWS(domain.VenueDeribit, streamState(private.Connected(), unixMilliseconds(privateMetrics.LastReceiveAt), now, 2*webConfig.AccountReconcileInterval), unixTime(privateEventMS.Load()), privateMetrics.Reconnects)
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

func unixMilliseconds(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

type valuationQueue struct {
	mu     sync.Mutex
	latest map[string]accountstate.USDPrice
}

func newValuationQueue() *valuationQueue {
	return &valuationQueue{latest: make(map[string]accountstate.USDPrice)}
}

func (q *valuationQueue) Store(update accountstate.USDPrice) {
	q.mu.Lock()
	q.latest[update.Asset] = update
	q.mu.Unlock()
}

func (q *valuationQueue) Drain() []accountstate.USDPrice {
	q.mu.Lock()
	assets := make([]string, 0, len(q.latest))
	for asset := range q.latest {
		assets = append(assets, asset)
	}
	sort.Strings(assets)
	batch := make([]accountstate.USDPrice, 0, len(assets))
	for _, asset := range assets {
		batch = append(batch, q.latest[asset])
		delete(q.latest, asset)
	}
	q.mu.Unlock()
	return batch
}

func runWebValuationUpdates(ctx context.Context, interval time.Duration, accounts *accountstate.Manager, updates *valuationQueue, logger *slog.Logger) {
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			batch := updates.Drain()
			if len(batch) == 0 {
				accounts.Revalue(now)
				continue
			}
			if err := accounts.UpdateUSDPrices(batch, now); err != nil {
				logger.Warn("public price update rejected")
			}
		}
	}
}

func decodeDeribitUSDPrice(event deribit.WSNotification) (accountstate.USDPrice, bool, error) {
	const prefix = "deribit_price_index."
	if !strings.HasPrefix(event.Channel, prefix) {
		return accountstate.USDPrice{}, false, nil
	}
	indexName := strings.TrimPrefix(event.Channel, prefix)
	asset := ""
	switch indexName {
	case "btc_usd":
		asset = "BTC"
	case "eth_usd":
		asset = "ETH"
	default:
		return accountstate.USDPrice{}, false, nil
	}
	var payload struct {
		Price     json.Number `json:"price"`
		Timestamp int64       `json:"timestamp"`
	}
	decoder := json.NewDecoder(bytes.NewReader(event.Data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return accountstate.USDPrice{}, true, fmt.Errorf("decode Deribit %s index price", indexName)
	}
	priceText := strings.TrimSpace(payload.Price.String())
	priceValue, validPrice := new(big.Rat).SetString(priceText)
	if !validPrice || priceValue.Sign() <= 0 {
		return accountstate.USDPrice{}, true, fmt.Errorf("decode Deribit %s index price", indexName)
	}
	observedAt := event.ReceivedAt
	if payload.Timestamp > 0 {
		observedAt = time.UnixMilli(payload.Timestamp).UTC()
	}
	return accountstate.USDPrice{
		Asset: asset, Value: priceText, Source: "Deribit Testnet " + indexName + " index",
		ObservedAt: observedAt, ReceivedAt: event.ReceivedAt,
	}, true, nil
}

func enqueueValuation(updates *valuationQueue, update accountstate.USDPrice) {
	updates.Store(update)
}

func recordDeribitOrderMetric(accounts *accountstate.Manager, event deribit.WSNotification) error {
	switch {
	case strings.HasPrefix(event.Channel, "user.orders."):
		var order deribit.Order
		if err := json.Unmarshal(event.Data, &order); err != nil {
			return fmt.Errorf("decode Deribit private order metric: %w", err)
		}
		accounts.RecordOrderEvent(domain.VenueDeribit, order.Label, order.OrderID, "order", event.ReceivedAt)
	case strings.HasPrefix(event.Channel, "user.trades."):
		var trades []deribit.Trade
		if err := json.Unmarshal(event.Data, &trades); err != nil {
			var trade deribit.Trade
			if singleErr := json.Unmarshal(event.Data, &trade); singleErr != nil {
				return fmt.Errorf("decode Deribit private trade metric: %w", err)
			}
			trades = []deribit.Trade{trade}
		}
		for _, trade := range trades {
			accounts.RecordOrderEvent(domain.VenueDeribit, "", trade.OrderID, "execution", event.ReceivedAt)
		}
	}
	return nil
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
