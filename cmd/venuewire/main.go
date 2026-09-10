package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"venuewire/internal/config"
	"venuewire/internal/observability"
)

const usage = `VenueWire - Bybit + Deribit Testnet connector

Usage:
  venuewire [--env-file PATH] web
  venuewire --venue bybit <command> [options]
  venuewire --venue deribit <command> [options]
  venuewire --venue all <command> [options]

Global options:
  --env-file PATH  Load dotenv values without overriding the OS environment

Public REST:
  venuewire --venue bybit time
  venuewire --venue bybit instrument [--category linear] [--symbol BTCUSDT]
  venuewire --venue bybit ticker [--category linear] [--symbol BTCUSDT]

Deribit Testnet JSON-RPC:
  venuewire --venue deribit doctor
  venuewire --venue deribit time
  venuewire --venue deribit instrument --name BTC-PERPETUAL
  venuewire --venue deribit ticker --instrument BTC-PERPETUAL
  venuewire --venue deribit instruments [--currency BTC] [--kind future]
  venuewire --venue deribit account balances [--currency all|BTC,ETH]
  venuewire --venue deribit positions [--currency BTC] [--kind future]
  venuewire --venue deribit public-stream [--channels trades.BTC-PERPETUAL.100ms,book.BTC-PERPETUAL.100ms] [--duration 30s]
  venuewire --venue deribit private-stream [--channels user.changes.any.any.raw] [--duration 30s] [--enable-connection-cod --confirm]
  venuewire --venue deribit order plan --instrument BTC-PERPETUAL --side buy --amount 10 --type limit --price PRICE [--transport http|ws|fix] [--post-only]
  venuewire --venue deribit order execute --plan-id ID --confirm
  venuewire --venue deribit order status --order-id ID
  venuewire --venue deribit order amend --order-id ID --amount AMOUNT --price PRICE [--transport http|ws|fix] --confirm
  venuewire --venue deribit order cancel --order-id ID [--transport http|ws|fix] --confirm
  venuewire --venue deribit order trades --order-id ID
  venuewire --venue deribit reconcile
  venuewire --venue deribit fix mock-demo
  venuewire --venue deribit fix connect-testnet [--duration 5s]
  venuewire --venue all status
  venuewire --venue all portfolio

Market WebSocket:
  venuewire --venue bybit market trades [--symbol BTCUSDT]
  venuewire --venue bybit market orderbook [--symbol BTCUSDT] [--depth 50]

Authenticated REST:
  venuewire --venue bybit account info
  venuewire --venue bybit account balances [--coin BTC,ETH,USDT]
  venuewire --venue bybit order place --side Buy|Sell --qty QTY [--category linear] [--symbol BTCUSDT] [--type Limit|Market] [--price PRICE] [--time-in-force GTC] [--reduce-only]
  venuewire --venue bybit order cancel [--category linear] [--symbol BTCUSDT] (--order-id ID | --order-link-id ID)
  venuewire --venue bybit order amend [--category linear] [--symbol BTCUSDT] (--order-id ID | --order-link-id ID) [--qty QTY] [--price PRICE]
  venuewire --venue bybit order status [--category linear] [--symbol BTCUSDT] [--order-id ID] [--order-link-id ID]
  venuewire --venue bybit executions [--category linear] [--symbol BTCUSDT] [--order-id ID] [--order-link-id ID]
  venuewire --venue bybit positions [--category linear] [--symbol BTCUSDT]

Private state and reconciliation:
  venuewire --venue bybit private-stream [--symbol BTCUSDT]
  venuewire --venue bybit reconcile [--category linear] [--symbol BTCUSDT]
  venuewire --venue bybit state migrate-v1 --bybit-account-alias ALIAS [--dry-run] [--backup PATH]
  venuewire --venue bybit state restore-v1 [--backup PATH]

FIX:
  venuewire --venue bybit fix mock-demo
  venuewire --venue bybit fix mock-server [--listen 127.0.0.1:9001] [--scenario accepted]
  venuewire --venue bybit fix connect-testnet

Help:
  venuewire help | --help | -h

Venue selection:
  --venue must appear before the command.
  Omitting --venue preserves legacy Bybit routing.

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
	loadedArgs, err := config.LoadEnvironment(args)
	if err != nil {
		observability.NewJSON(os.Stderr).Error("dotenv rejected", slog.String("error", err.Error()))
		return 2
	}
	args = loadedArgs
	args = normalizeVenueArgs(args)
	cfg := config.Load()
	logger := observability.NewJSON(os.Stderr, cfg.APISecret, cfg.Deribit.APISecret)

	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(os.Stdout, usage)
		return 0
	}
	if handled, err := executeWebCommand(ctx, cfg, logger, args); handled {
		if err != nil {
			logger.Error("web command failed", slog.String("error", err.Error()))
			return 2
		}
		return 0
	}

	if err := cfg.ValidateTestnet(); err != nil {
		logger.Error("configuration rejected", slog.String("error", err.Error()))
		return 2
	}
	if isDeribitCommand(args) && !isDeribitLocalMockCommand(args) {
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
	if handled, err := executeMultiVenueCommand(ctx, cfg, args, os.Stdout); handled {
		if err != nil {
			logger.Error("multi-venue command failed", slog.String("error", err.Error()))
			return 1
		}
		return 0
	}
	if handled, err := executeDeribitFIXCommand(ctx, cfg, logger, args, os.Stdout); handled {
		if err != nil {
			logger.Error("Deribit FIX command failed", slog.String("error", err.Error()))
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

func normalizeVenueArgs(args []string) []string {
	if len(args) < 3 || args[0] != "--venue" {
		return args
	}
	switch args[1] {
	case "bybit":
		return args[2:]
	case "deribit", "all":
		return append([]string{"venue", args[1]}, args[2:]...)
	default:
		return args
	}
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

func isDeribitLocalMockCommand(args []string) bool {
	return len(args) == 4 && isDeribitCommand(args) && args[2] == "fix" && args[3] == "mock-demo"
}

func isDeribitAuthenticatedCommand(args []string) bool {
	return len(args) >= 3 && isDeribitCommand(args) && (args[2] == "account" || args[2] == "positions" || args[2] == "private-stream" || args[2] == "order" || args[2] == "reconcile" || (args[2] == "fix" && len(args) >= 4 && args[3] == "connect-testnet"))
}
