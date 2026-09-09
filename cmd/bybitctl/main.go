package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"bybit/internal/config"
	"bybit/internal/observability"
)

const usage = `bybitctl - Bybit Testnet connectivity lab

Usage:
  bybitctl <command> [options]

Public REST:
  bybitctl time
  bybitctl instrument [--category linear] [--symbol BTCUSDT]

Deribit Testnet JSON-RPC:
  bybitctl venue deribit time
  bybitctl venue deribit instrument --name BTC-PERPETUAL
  bybitctl venue deribit ticker --instrument BTC-PERPETUAL
  bybitctl venue deribit instruments [--currency BTC] [--kind future]
  bybitctl venue deribit account balances [--currency all|BTC,ETH]
  bybitctl venue deribit positions [--currency BTC] [--kind future]
  bybitctl venue deribit public-stream [--channels trades.BTC-PERPETUAL.100ms,book.BTC-PERPETUAL.100ms] [--duration 30s]
  bybitctl venue deribit private-stream [--channels user.changes.any.any.raw] [--duration 30s]
  bybitctl venue deribit order plan --instrument BTC-PERPETUAL --side buy --amount 10 --type limit --price PRICE [--transport http|ws] [--post-only]
  bybitctl venue deribit order execute --plan-id ID --confirm
  bybitctl venue deribit order status --order-id ID
  bybitctl venue deribit order amend --order-id ID --amount AMOUNT --price PRICE --confirm
  bybitctl venue deribit order cancel --order-id ID --confirm
  bybitctl venue deribit order trades --order-id ID
  bybitctl venue deribit reconcile

Market WebSocket:
  bybitctl market trades [--symbol BTCUSDT]
  bybitctl market orderbook [--symbol BTCUSDT] [--depth 50]

Authenticated REST:
  bybitctl account info
  bybitctl account balances [--coin BTC,ETH,USDT]
  bybitctl order place --side Buy|Sell --qty QTY [--category linear] [--symbol BTCUSDT] [--type Limit|Market] [--price PRICE] [--time-in-force GTC]
  bybitctl order cancel [--category linear] [--symbol BTCUSDT] (--order-id ID | --order-link-id ID)
  bybitctl order amend [--category linear] [--symbol BTCUSDT] (--order-id ID | --order-link-id ID) [--qty QTY] [--price PRICE]
  bybitctl order status [--category linear] [--symbol BTCUSDT] [--order-id ID] [--order-link-id ID]
  bybitctl executions [--category linear] [--symbol BTCUSDT] [--order-id ID] [--order-link-id ID]
  bybitctl positions [--category linear] [--symbol BTCUSDT]

Private state and reconciliation:
  bybitctl private-stream [--symbol BTCUSDT]
  bybitctl reconcile [--category linear] [--symbol BTCUSDT]
  bybitctl state migrate-v1 --bybit-account-alias ALIAS [--dry-run] [--backup PATH]
  bybitctl state restore-v1 [--backup PATH]

FIX:
  bybitctl fix mock-demo
  bybitctl fix mock-server [--listen 127.0.0.1:9001] [--scenario accepted]
  bybitctl fix connect-testnet

Help:
  bybitctl help | --help | -h

Safety:
  All authenticated commands fail closed unless exact Testnet endpoints and the
  required credentials are configured. Mainnet hosts are always rejected.`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:]))
}

func run(args []string) int {
	return runContext(context.Background(), args)
}

func runContext(ctx context.Context, args []string) int {
	cfg := config.Load()
	logger := observability.NewJSON(os.Stderr, cfg.APISecret, cfg.Deribit.APISecret)

	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(os.Stdout, usage)
		return 0
	}

	if err := cfg.ValidateTestnet(); err != nil {
		logger.Error("configuration rejected", slog.String("error", err.Error()))
		return 2
	}
	if isDeribitCommand(args) {
		if !cfg.Deribit.Enabled {
			logger.Error("Deribit command refused", slog.String("error", "DERIBIT_ENABLED must be true"))
			return 2
		}
		if err := cfg.Deribit.ValidateTestnet(); err != nil {
			logger.Error("Deribit configuration rejected", slog.String("error", err.Error()))
			return 2
		}
		if isDeribitAuthenticatedCommand(args) {
			if err := cfg.Deribit.RequireCredentials(); err != nil {
				logger.Error("Deribit authenticated command refused", slog.String("error", err.Error()))
				return 2
			}
		}
	}

	if isRESTAuthenticatedCommand(args) {
		if err := cfg.RequireRESTCredentials(); err != nil {
			logger.Error("authenticated command refused", slog.String("error", err.Error()))
			return 2
		}
	}
	if isFIXAuthenticatedCommand(args) {
		if err := cfg.RequireFIXCredentials(); err != nil {
			logger.Error("authenticated FIX command refused", slog.String("error", err.Error()))
			return 2
		}
	}

	if handled, err := executeRESTCommand(ctx, cfg, logger, args, os.Stdout); handled {
		if err != nil {
			logger.Error("command failed", slog.String("error", err.Error()))
			return 1
		}
		return 0
	}
	if handled, err := executeDeribitCommand(ctx, cfg, args, os.Stdout); handled {
		if err != nil {
			logger.Error("Deribit command failed", slog.String("error", err.Error()))
			return 1
		}
		return 0
	}
	if handled, err := executeStateCommand(ctx, cfg, args, os.Stdout); handled {
		if err != nil {
			logger.Error("state command failed", slog.String("error", err.Error()))
			return 1
		}
		return 0
	}
	if handled, err := executeReconcileCommand(ctx, cfg, logger, args, os.Stdout); handled {
		if err != nil {
			logger.Error("reconciliation failed", slog.String("error", err.Error()))
			return 1
		}
		return 0
	}
	if handled, err := executeMarketCommand(ctx, cfg, logger, args, os.Stdout); handled {
		if err != nil {
			logger.Error("market stream failed", slog.String("error", err.Error()))
			return 1
		}
		return 0
	}
	if handled, err := executePrivateStreamCommand(ctx, cfg, logger, args, os.Stdout); handled {
		if err != nil {
			logger.Error("private stream failed", slog.String("error", err.Error()))
			return 1
		}
		return 0
	}
	if handled, err := executeFIXCommand(ctx, cfg, logger, args, os.Stdout); handled {
		if err != nil {
			logger.Error("FIX command failed", slog.String("error", err.Error()))
			return 1
		}
		return 0
	}

	logger.Error("command is not implemented in the current phase", slog.String("command", strings.Join(args, " ")))
	return 1
}

func isRESTAuthenticatedCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "account", "order", "executions", "positions", "private-stream", "reconcile":
		return true
	default:
		return false
	}
}

func isFIXAuthenticatedCommand(args []string) bool {
	return len(args) >= 2 && args[0] == "fix" && args[1] == "connect-testnet"
}

func isDeribitCommand(args []string) bool {
	return len(args) >= 2 && args[0] == "venue" && args[1] == "deribit"
}

func isDeribitAuthenticatedCommand(args []string) bool {
	return len(args) >= 3 && isDeribitCommand(args) && (args[2] == "account" || args[2] == "positions" || args[2] == "private-stream" || args[2] == "order" || args[2] == "reconcile")
}
