package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"bybit/internal/config"
	"bybit/internal/domain"
	"bybit/internal/orderstate"
	"bybit/internal/rest"
)

func executeRESTCommand(ctx context.Context, cfg config.Config, logger *slog.Logger, args []string, output io.Writer) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	client := rest.NewClient(cfg.RESTBaseURL, cfg.APIKey, cfg.APISecret)
	switch args[0] {
	case "time":
		serverTime, meta, err := client.ServerTime(ctx)
		if err != nil {
			return true, err
		}
		skew := time.Now().UTC().Sub(time.UnixMilli(serverTime.UnixMilli)).Abs()
		if skew > 2*time.Second {
			logger.Warn("material clock skew", slog.Duration("skew", skew))
		}
		logRateLimit(logger, meta)
		return true, writeJSON(output, serverTime)
	case "instrument":
		flags := newFlagSet("instrument")
		category := flags.String("category", "linear", "product category")
		symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
		if err := flags.Parse(args[1:]); err != nil {
			return true, err
		}
		instruments, meta, err := client.Instruments(ctx, *category, *symbol)
		logRateLimit(logger, meta)
		if err != nil {
			return true, err
		}
		return true, writeJSON(output, instruments)
	case "ticker":
		flags := newFlagSet("ticker")
		category := flags.String("category", "linear", "product category")
		symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
		if err := flags.Parse(args[1:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 {
			return true, fmt.Errorf("unexpected ticker arguments: %v", flags.Args())
		}
		tickers, meta, err := client.Tickers(ctx, *category, *symbol)
		logRateLimit(logger, meta)
		if err != nil {
			return true, err
		}
		return true, writeJSON(output, tickers)
	case "order":
		return true, executeOrderCommand(ctx, cfg, logger, client, args[1:], output)
	case "executions":
		flags := newFlagSet("executions")
		category := flags.String("category", "linear", "product category")
		symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
		orderID := flags.String("order-id", "", "exchange order ID")
		orderLinkID := flags.String("order-link-id", "", "client order link ID")
		if err := flags.Parse(args[1:]); err != nil {
			return true, err
		}
		executions, meta, err := client.Executions(ctx, *category, *symbol, *orderID, *orderLinkID)
		logRateLimit(logger, meta)
		if err != nil {
			return true, err
		}
		return true, writeJSON(output, executions)
	case "positions":
		flags := newFlagSet("positions")
		category := flags.String("category", "linear", "product category")
		symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
		if err := flags.Parse(args[1:]); err != nil {
			return true, err
		}
		positions, meta, err := client.Positions(ctx, *category, *symbol)
		logRateLimit(logger, meta)
		if err != nil {
			return true, err
		}
		return true, writeJSON(output, positions)
	case "account":
		return true, executeAccountCommand(ctx, logger, client, args[1:], output)
	default:
		return false, nil
	}
}

func executeAccountCommand(ctx context.Context, logger *slog.Logger, client *rest.Client, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("account command requires info or balances")
	}
	switch args[0] {
	case "info":
		flags := newFlagSet("account info")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		info, meta, err := client.AccountInfo(ctx)
		logRateLimit(logger, meta)
		if err != nil {
			return err
		}
		return writeJSON(output, info)
	case "balances":
		flags := newFlagSet("account balances")
		coins := flags.String("coin", "", "optional comma-separated coin symbols; omitted returns all nonzero balances")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		balances, meta, err := client.WalletBalances(ctx, *coins)
		logRateLimit(logger, meta)
		if err != nil {
			return err
		}
		return writeJSON(output, balances)
	default:
		return fmt.Errorf("unknown account command %q", args[0])
	}
}

func executeOrderCommand(ctx context.Context, cfg config.Config, logger *slog.Logger, client *rest.Client, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("order command requires one of: place, cancel, amend, status")
	}
	switch args[0] {
	case "place":
		flags := newFlagSet("order place")
		category := flags.String("category", "linear", "product category")
		symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
		side := flags.String("side", "", "Buy or Sell")
		orderType := flags.String("type", "Limit", "Limit or Market")
		qty := flags.String("qty", "", "decimal quantity")
		price := flags.String("price", "", "decimal limit price")
		tif := flags.String("time-in-force", "GTC", "time in force")
		linkID := flags.String("order-link-id", "", "optional unique client identifier")
		reduceOnly := flags.Bool("reduce-only", false, "reduce an existing position only")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *side == "" || *qty == "" || (*orderType == "Limit" && *price == "") {
			return errors.New("--side and --qty are required; --price is required for Limit orders")
		}
		if *linkID == "" {
			generated, err := rest.GenerateOrderLinkID(time.Now())
			if err != nil {
				return err
			}
			*linkID = generated
		}
		request := rest.PlaceOrderRequest{Category: *category, Symbol: *symbol, Side: *side, OrderType: *orderType, Qty: *qty, Price: *price, TimeInForce: *tif, OrderLinkID: *linkID, ReduceOnly: *reduceOnly}
		store := orderstate.FileStore{Path: cfg.StateFile}
		now := time.Now().UTC()
		local := domain.Order{Exchange: "bybit", Category: *category, Symbol: *symbol, OrderLinkID: *linkID, Side: domain.Side(*side), Type: domain.OrderType(*orderType), Qty: *qty, Price: *price, Status: domain.OrderStatusPendingSubmit, RawStatus: "LOCAL_PENDING_SUBMIT", CreatedAt: now, UpdatedAt: now}
		if err := persistOrder(ctx, store, local); err != nil {
			return fmt.Errorf("persist pending order before submission: %w", err)
		}
		ack, meta, err := client.PlaceOrder(ctx, request)
		logRateLimit(logger, meta)
		if err != nil {
			var uncertain *rest.UncertainSubmissionError
			if errors.As(err, &uncertain) {
				local.RawStatus = "REST_SUBMISSION_UNCERTAIN"
				local.UpdatedAt = time.Now().UTC()
				if persistErr := persistOrder(context.WithoutCancel(ctx), store, local); persistErr != nil {
					logger.Error("failed to persist uncertain REST submission",
						slog.String("orderLinkId", local.OrderLinkID),
						slog.String("error", persistErr.Error()))
				}
			} else {
				var apiError *rest.APIError
				if errors.As(err, &apiError) {
					local.Status = domain.OrderStatusRejected
					local.RawStatus = fmt.Sprintf("REST_REJECTED_%d", apiError.Code)
					local.UpdatedAt = time.Now().UTC()
					if persistErr := persistOrder(context.WithoutCancel(ctx), store, local); persistErr != nil {
						logger.Error("failed to persist REST rejection",
							slog.String("orderLinkId", local.OrderLinkID),
							slog.String("error", persistErr.Error()))
					}
				}
			}
			return err
		}
		local.OrderID = ack.OrderID
		local.OrderLinkID = ack.OrderLinkID
		local.RawStatus = "REST_ACK"
		local.UpdatedAt = time.Now().UTC()
		logger.Info("order submission acknowledged; final state pending private stream or reconciliation",
			slog.String("protocol", "REST"),
			slog.String("symbol", *symbol),
			slog.String("orderId", ack.OrderID),
			slog.String("orderLinkId", ack.OrderLinkID))
		if err := persistOrder(ctx, store, local); err != nil {
			return fmt.Errorf(
				"persist REST acknowledgement for orderId=%q orderLinkId=%q: %w",
				ack.OrderID, ack.OrderLinkID, err,
			)
		}
		return writeJSON(output, ack)
	case "cancel":
		flags := newFlagSet("order cancel")
		category := flags.String("category", "linear", "product category")
		symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
		orderID := flags.String("order-id", "", "exchange order ID")
		linkID := flags.String("order-link-id", "", "client order link ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *orderID == "" && *linkID == "" {
			return errors.New("one of --order-id or --order-link-id is required")
		}
		ack, meta, err := client.CancelOrder(ctx, rest.CancelOrderRequest{Category: *category, Symbol: *symbol, OrderID: *orderID, OrderLinkID: *linkID})
		logRateLimit(logger, meta)
		if err != nil {
			return err
		}
		logger.Info("order cancellation acknowledged; final state pending private stream or reconciliation", slog.String("protocol", "REST"), slog.String("symbol", *symbol), slog.String("orderId", ack.OrderID), slog.String("orderLinkId", ack.OrderLinkID))
		return writeJSON(output, ack)
	case "amend":
		flags := newFlagSet("order amend")
		category := flags.String("category", "linear", "product category")
		symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
		orderID := flags.String("order-id", "", "exchange order ID")
		linkID := flags.String("order-link-id", "", "client order link ID")
		qty := flags.String("qty", "", "new quantity")
		price := flags.String("price", "", "new price")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if (*orderID == "" && *linkID == "") || (*qty == "" && *price == "") {
			return errors.New("an order identifier and at least one of --qty or --price are required")
		}
		ack, meta, err := client.AmendOrder(ctx, rest.AmendOrderRequest{Category: *category, Symbol: *symbol, OrderID: *orderID, OrderLinkID: *linkID, Qty: *qty, Price: *price})
		logRateLimit(logger, meta)
		if err != nil {
			return err
		}
		return writeJSON(output, ack)
	case "status":
		flags := newFlagSet("order status")
		category := flags.String("category", "linear", "product category")
		symbol := flags.String("symbol", "BTCUSDT", "instrument symbol")
		orderID := flags.String("order-id", "", "exchange order ID")
		linkID := flags.String("order-link-id", "", "client order link ID")
		openOnlyValue := flags.String("open-only", "", "Bybit openOnly value (0, 1, or 2)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		var openOnly *int
		if *openOnlyValue != "" {
			parsed, err := strconv.Atoi(*openOnlyValue)
			if err != nil {
				return fmt.Errorf("invalid --open-only: %w", err)
			}
			openOnly = &parsed
		}
		orders, meta, err := client.Orders(ctx, *category, *symbol, *orderID, *linkID, openOnly)
		logRateLimit(logger, meta)
		if err != nil {
			return err
		}
		return writeJSON(output, orders)
	default:
		return fmt.Errorf("unknown order command %q", args[0])
	}
}

func persistOrder(ctx context.Context, store orderstate.FileStore, order domain.Order) error {
	service, err := orderstate.NewService(ctx, store)
	if err != nil {
		return err
	}
	return service.UpsertREST(ctx, order)
}

func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func logRateLimit(logger *slog.Logger, meta rest.ResponseMeta) {
	if meta.RateLimit.Limit == 0 && meta.RateLimit.Remaining == 0 {
		return
	}
	logger.Info("Bybit rate limit", slog.String("protocol", "REST"), slog.Int64("latency_ms", meta.Latency.Milliseconds()), slog.Int64("rate_limit", meta.RateLimit.Limit), slog.Int64("rate_limit_remaining", meta.RateLimit.Remaining), slog.Time("rate_limit_reset", meta.RateLimit.ResetAt))
}
