package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"bybit/internal/config"
	bybitws "bybit/internal/ws"
)

func executeMarketCommand(ctx context.Context, cfg config.Config, logger *slog.Logger, args []string, output io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "market" {
		return false, nil
	}
	if len(args) < 2 {
		return true, errors.New("market command requires trades or orderbook")
	}
	flags := newFlagSet("market " + args[1])
	symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
	depth := flags.Int("depth", 50, "order book depth")
	if err := flags.Parse(args[2:]); err != nil {
		return true, err
	}
	var topic string
	switch args[1] {
	case "trades":
		topic = "publicTrade." + *symbol
	case "orderbook":
		if *depth <= 0 {
			return true, errors.New("--depth must be positive")
		}
		topic = "orderbook." + strconv.Itoa(*depth) + "." + *symbol
	default:
		return true, fmt.Errorf("unknown market command %q", args[1])
	}
	client := bybitws.NewClient(bybitws.Config{URL: cfg.WSPublicURL, Topics: []string{topic}})
	logger.Info("public WebSocket stream starting", slog.String("protocol", "WS"), slog.String("topic", topic))
	err := client.Run(ctx, func(_ context.Context, event bybitws.Event) error {
		logger.Info("public WebSocket event", slog.String("protocol", "WS"), slog.String("topic", event.Topic), slog.String("symbol", event.Symbol), slog.Time("exchange_timestamp", event.ExchangeTime), slog.Time("receive_timestamp", event.ReceivedAt), slog.Int64("lag_ms", event.Lag.Milliseconds()))
		return writeJSON(output, event)
	})
	stats := client.Stats()
	logger.Info("public WebSocket stream stopped", slog.String("protocol", "WS"), slog.Uint64("messages", stats.Messages), slog.Uint64("reconnects", stats.Reconnects), slog.Uint64("dropped", stats.Dropped), slog.Uint64("decode_errors", stats.DecodeErrors))
	return true, err
}
