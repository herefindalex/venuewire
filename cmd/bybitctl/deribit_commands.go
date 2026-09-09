package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"bybit/internal/config"
	"bybit/internal/deribit"
	"bybit/internal/deribitreconcile"
	"bybit/internal/intent"
	"bybit/internal/orderstate"
)

func executeDeribitCommand(ctx context.Context, cfg config.Config, args []string, output io.Writer) (bool, error) {
	if !isDeribitCommand(args) {
		return false, nil
	}
	if len(args) < 3 {
		return true, errors.New("Deribit command is required")
	}
	client, err := deribit.NewClient(cfg.Deribit.HTTPBaseURL, cfg.Deribit.APIKey, cfg.Deribit.APISecret, nil)
	if err != nil {
		return true, err
	}
	encoder := json.NewEncoder(output)
	switch args[2] {
	case "doctor":
		if len(args) != 3 {
			return true, errors.New("doctor takes no arguments")
		}
		serverTime, err := client.ServerTime(ctx)
		if err != nil {
			return true, err
		}
		instrument, err := client.Instrument(ctx, "BTC-PERPETUAL")
		if err != nil {
			return true, err
		}
		result := map[string]any{"venue": "deribit", "environment": "testnet", "accountAlias": cfg.Deribit.AccountAlias, "public": map[string]any{"ok": true, "timeMs": serverTime, "instrument": instrument.InstrumentName, "active": instrument.IsActive}, "tradingWritePerformed": false}
		if err := cfg.Deribit.RequireCredentials(); err != nil {
			result["private"] = map[string]any{"ok": false, "error": err.Error()}
		} else {
			summaries, err := client.AccountSummaries(ctx)
			if err != nil {
				result["private"] = map[string]any{"ok": false, "error": err.Error()}
			} else {
				result["private"] = map[string]any{"ok": true, "currencies": len(summaries)}
			}
		}
		return true, encoder.Encode(result)
	case "time":
		if len(args) != 3 {
			return true, errors.New("time takes no arguments")
		}
		milliseconds, err := client.ServerTime(ctx)
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(map[string]any{"venue": "deribit", "environment": "testnet", "timeMs": milliseconds, "time": time.UnixMilli(milliseconds).UTC()})
	case "instrument":
		flags := flag.NewFlagSet("venue deribit instrument", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		name := flags.String("name", "", "instrument name")
		if err := flags.Parse(args[3:]); err != nil {
			return true, err
		}
		if *name == "" || flags.NArg() != 0 {
			return true, errors.New("instrument requires --name and no positional arguments")
		}
		result, err := client.Instrument(ctx, *name)
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(result)
	case "ticker":
		flags := flag.NewFlagSet("venue deribit ticker", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		instrumentName := flags.String("instrument", "", "instrument")
		if err := flags.Parse(args[3:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 || *instrumentName == "" {
			return true, errors.New("ticker requires --instrument and no positional arguments")
		}
		result, err := client.Ticker(ctx, *instrumentName)
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(result)
	case "instruments":
		flags := flag.NewFlagSet("venue deribit instruments", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		currency := flags.String("currency", "BTC", "currency")
		kind := flags.String("kind", "future", "kind")
		expired := flags.Bool("expired", false, "include expired")
		if err := flags.Parse(args[3:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 {
			return true, fmt.Errorf("unexpected arguments: %v", flags.Args())
		}
		result, err := client.Instruments(ctx, strings.ToUpper(*currency), *kind, *expired)
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(result)
	case "account":
		if len(args) < 4 || args[3] != "balances" {
			return true, errors.New("account requires balances")
		}
		flags := flag.NewFlagSet("venue deribit account balances", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		currencies := flags.String("currency", "all", "all or comma-separated currencies")
		if err := flags.Parse(args[4:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 {
			return true, fmt.Errorf("unexpected arguments: %v", flags.Args())
		}
		if strings.EqualFold(strings.TrimSpace(*currencies), "all") {
			results, err := client.AccountSummaries(ctx)
			if err != nil {
				return true, err
			}
			return true, encoder.Encode(results)
		}
		list, err := deribitCurrencies(ctx, client, *currencies)
		if err != nil {
			return true, err
		}
		results := make([]deribit.AccountSummary, 0, len(list))
		for _, currency := range list {
			summary, err := client.AccountSummary(ctx, currency)
			if err != nil {
				return true, fmt.Errorf("account summary %s: %w", currency, err)
			}
			results = append(results, summary)
		}
		return true, encoder.Encode(results)
	case "positions":
		flags := flag.NewFlagSet("venue deribit positions", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		currency := flags.String("currency", "BTC", "currency")
		kind := flags.String("kind", "future", "kind")
		if err := flags.Parse(args[3:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 {
			return true, fmt.Errorf("unexpected arguments: %v", flags.Args())
		}
		result, err := client.Positions(ctx, strings.ToUpper(*currency), *kind)
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(result)
	case "public-stream", "private-stream":
		flags := flag.NewFlagSet("venue deribit "+args[2], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		defaultChannels := "trades.BTC-PERPETUAL.100ms,book.BTC-PERPETUAL.100ms"
		private := args[2] == "private-stream"
		if private {
			defaultChannels = "user.changes.any.any.raw"
		}
		channelsRaw := flags.String("channels", defaultChannels, "comma-separated channels")
		duration := flags.Duration("duration", 30*time.Second, "bounded stream duration")
		if err := flags.Parse(args[3:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 || *duration <= 0 {
			return true, errors.New("stream requires a positive duration and no positional arguments")
		}
		channels, err := splitNonempty(*channelsRaw)
		if err != nil {
			return true, err
		}
		streamConfig := deribit.WSConfig{URL: cfg.Deribit.WSURL, Channels: channels, Private: private}
		if private {
			reconciler, err := newDeribitReconciler(ctx, cfg, client)
			if err != nil {
				return true, err
			}
			streamConfig.OnReady = func(readyCtx context.Context, _ uint64) error { _, err := reconciler.Run(readyCtx); return err }
		}
		stream, err := deribit.NewWSClient(client, streamConfig)
		if err != nil {
			return true, err
		}
		streamCtx, cancel := context.WithTimeout(ctx, *duration)
		defer cancel()
		err = stream.Run(streamCtx, func(_ context.Context, event deribit.WSNotification) error { return encoder.Encode(event) })
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return true, err
		}
		metrics := stream.Metrics()
		if metrics.Ready == 0 {
			return true, fmt.Errorf("stream ended without notifications: %s", metrics.LastError)
		}
		return true, encoder.Encode(map[string]any{"complete": true, "metrics": metrics})
	case "market":
		if len(args) < 4 {
			return true, errors.New("market requires trades or orderbook")
		}
		flags := flag.NewFlagSet("venue deribit market", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		instrumentName := flags.String("instrument", "BTC-PERPETUAL", "instrument")
		duration := flags.Duration("duration", 30*time.Second, "duration")
		depth := flags.Int("depth", 10, "order-book depth")
		if err := flags.Parse(args[4:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 {
			return true, fmt.Errorf("unexpected arguments: %v", flags.Args())
		}
		var channel string
		switch args[3] {
		case "trades":
			channel = "trades." + *instrumentName + ".100ms"
		case "orderbook":
			if *depth <= 0 {
				return true, errors.New("depth must be positive")
			}
			channel = fmt.Sprintf("book.%s.none.%d.100ms", *instrumentName, *depth)
		default:
			return true, fmt.Errorf("unknown market command %q", args[3])
		}
		return executeDeribitCommand(ctx, cfg, []string{"venue", "deribit", "public-stream", "--channels", channel, "--duration", duration.String()}, output)
	case "orders":
		if len(args) < 4 || args[3] != "list" {
			return true, errors.New("orders requires list")
		}
		flags := flag.NewFlagSet("venue deribit orders list", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		instrumentName := flags.String("instrument", "BTC-PERPETUAL", "instrument")
		if err := flags.Parse(args[4:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 {
			return true, fmt.Errorf("unexpected arguments: %v", flags.Args())
		}
		orders, err := client.OpenOrders(ctx, *instrumentName)
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(orders)
	case "order":
		if len(args) < 4 {
			return true, errors.New("order requires plan or execute")
		}
		reconciler, err := newDeribitReconciler(ctx, cfg, client)
		if err != nil {
			return true, err
		}
		if _, err := reconciler.RecoverUncertain(ctx); err != nil {
			return true, fmt.Errorf("pre-trade recovery failed: %w", err)
		}
		planner := &intent.Planner{Market: client, Store: intent.Store{Path: cfg.IntentFile}, Limits: intent.Limits{MaxOrderUSD: cfg.Deribit.Risk.MaxOrderUSD, MaxAggregateOpenUSD: cfg.Deribit.Risk.MaxAggregateOpenUSD, MaxPriceDeviationPct: cfg.Deribit.Risk.MaxPriceDeviationPct, MaxOpenOrders: cfg.Deribit.Risk.MaxOpenOrders, TTL: cfg.Deribit.PlanTTL}, AccountAlias: cfg.Deribit.AccountAlias, WSURL: cfg.Deribit.WSURL}
		planner.FIXPlace = func(fixCtx context.Context, plan intent.Plan) (deribit.OrderResult, error) {
			return placeDeribitFIX(fixCtx, cfg, client, plan)
		}
		switch args[3] {
		case "plan":
			flags := flag.NewFlagSet("venue deribit order plan", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			instrumentName := flags.String("instrument", "", "instrument")
			side := flags.String("side", "", "buy or sell")
			amount := flags.String("amount", "", "USD notional amount")
			orderType := flags.String("type", "limit", "limit or market")
			transport := flags.String("transport", "http", "http, ws, or fix")
			price := flags.String("price", "", "limit price")
			tif := flags.String("time-in-force", "good_til_cancelled", "time in force")
			postOnly := flags.Bool("post-only", false, "post only")
			reduceOnly := flags.Bool("reduce-only", false, "reduce only")
			if err := flags.Parse(args[4:]); err != nil {
				return true, err
			}
			if flags.NArg() != 0 {
				return true, fmt.Errorf("unexpected arguments: %v", flags.Args())
			}
			plan, err := planner.Create(ctx, intent.Request{Instrument: *instrumentName, Side: *side, OrderType: *orderType, Transport: *transport, Amount: *amount, Price: *price, TimeInForce: *tif, PostOnly: *postOnly, ReduceOnly: *reduceOnly})
			if err != nil {
				return true, err
			}
			return true, encoder.Encode(plan)
		case "execute":
			flags := flag.NewFlagSet("venue deribit order execute", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			planID := flags.String("plan-id", "", "persisted plan ID")
			confirm := flags.Bool("confirm", false, "confirm write")
			if err := flags.Parse(args[4:]); err != nil {
				return true, err
			}
			if flags.NArg() != 0 || *planID == "" {
				return true, errors.New("execute requires --plan-id and no positional arguments")
			}
			if *confirm {
				stored, readErr := planner.Store.Get(ctx, *planID)
				if readErr != nil {
					return true, readErr
				}
				if stored.Transport == "fix" {
					if gateErr := requireDeribitFIXTradingGates(cfg); gateErr != nil {
						return true, gateErr
					}
				}
			}
			result, err := planner.Execute(ctx, *planID, *confirm)
			if err != nil {
				return true, err
			}
			return true, encoder.Encode(result)
		case "status":
			flags := flag.NewFlagSet("venue deribit order status", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			orderID := flags.String("order-id", "", "native order ID")
			if err := flags.Parse(args[4:]); err != nil {
				return true, err
			}
			if flags.NArg() != 0 || *orderID == "" {
				return true, errors.New("status requires --order-id")
			}
			state, err := client.OrderState(ctx, *orderID)
			if err != nil {
				return true, err
			}
			return true, encoder.Encode(state)
		case "trades":
			flags := flag.NewFlagSet("venue deribit order trades", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			orderID := flags.String("order-id", "", "native order ID")
			if err := flags.Parse(args[4:]); err != nil {
				return true, err
			}
			if flags.NArg() != 0 || *orderID == "" {
				return true, errors.New("trades requires --order-id")
			}
			trades, err := client.TradesByOrder(ctx, *orderID)
			if err != nil {
				return true, err
			}
			return true, encoder.Encode(trades)
		case "amend":
			flags := flag.NewFlagSet("venue deribit order amend", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			orderID := flags.String("order-id", "", "native order ID")
			amount := flags.String("amount", "", "new USD amount")
			price := flags.String("price", "", "new limit price")
			transport := flags.String("transport", "http", "http or fix")
			confirm := flags.Bool("confirm", false, "confirm write")
			if err := flags.Parse(args[4:]); err != nil {
				return true, err
			}
			if flags.NArg() != 0 || *orderID == "" || *amount == "" || *price == "" {
				return true, errors.New("amend requires --order-id, --amount, --price, and no positional arguments")
			}
			if !*confirm {
				return true, errors.New("amend requires --confirm")
			}
			before, err := client.OrderState(ctx, *orderID)
			if err != nil {
				return true, err
			}
			if before.OrderState != "open" && before.OrderState != "untriggered" {
				return true, fmt.Errorf("order is not amendable: %s", before.OrderState)
			}
			if _, err := planner.ValidateRequest(ctx, intent.Request{Instrument: before.InstrumentName, Side: before.Direction, OrderType: "limit", Transport: *transport, Amount: *amount, Price: *price, ReduceOnly: before.ReduceOnly}); err != nil {
				return true, fmt.Errorf("amend risk validation: %w", err)
			}
			var ack any
			switch strings.ToLower(*transport) {
			case "http":
				ack, err = client.Edit(ctx, *orderID, *amount, *price)
			case "fix":
				ownedPlan, ownershipErr := requireConnectorOwnedFIXOrder(ctx, planner.Store, before)
				if ownershipErr != nil {
					return true, ownershipErr
				}
				ack, err = amendDeribitFIX(ctx, cfg, client, before, ownedPlan, *amount, *price)
			default:
				return true, errors.New("amend transport must be http or fix")
			}
			if err != nil {
				return true, err
			}
			after, readErr := client.OrderState(ctx, *orderID)
			if readErr != nil {
				return true, fmt.Errorf("amend independent verification: %w", readErr)
			}
			verified := after.OrderID == *orderID && decimalEqual(after.Amount.String(), *amount) && decimalEqual(after.Price.String(), *price)
			if !verified {
				return true, errors.New("amend independent verification mismatch")
			}
			return true, encoder.Encode(map[string]any{"acknowledgement": ack, "readState": after, "verified": true})
		case "cancel":
			flags := flag.NewFlagSet("venue deribit order cancel", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			orderID := flags.String("order-id", "", "native order ID")
			transport := flags.String("transport", "http", "http or fix")
			confirm := flags.Bool("confirm", false, "confirm write")
			if err := flags.Parse(args[4:]); err != nil {
				return true, err
			}
			if flags.NArg() != 0 || *orderID == "" {
				return true, errors.New("cancel requires --order-id and no positional arguments")
			}
			if !*confirm {
				return true, errors.New("cancel requires --confirm")
			}
			var ack any
			switch strings.ToLower(*transport) {
			case "http":
				ack, err = client.Cancel(ctx, *orderID)
			case "fix":
				before, readErr := client.OrderState(ctx, *orderID)
				if readErr != nil {
					return true, fmt.Errorf("FIX cancel independent pre-read: %w", readErr)
				}
				if before.OrderState != "open" && before.OrderState != "untriggered" {
					return true, fmt.Errorf("order is not cancellable: %s", before.OrderState)
				}
				if _, ownershipErr := requireConnectorOwnedFIXOrder(ctx, planner.Store, before); ownershipErr != nil {
					return true, ownershipErr
				}
				ack, err = cancelDeribitFIX(ctx, cfg, client, before)
			default:
				return true, errors.New("cancel transport must be http or fix")
			}
			if err != nil {
				return true, err
			}
			verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			var after deribit.Order
			for {
				after, err = client.OrderState(verifyCtx, *orderID)
				if err == nil && (after.OrderState == "cancelled" || after.OrderState == "filled") {
					break
				}
				select {
				case <-verifyCtx.Done():
					if err != nil {
						return true, fmt.Errorf("cancel independent verification: %w", err)
					}
					return true, errors.New("cancel independent verification did not reach terminal state")
				case <-time.After(100 * time.Millisecond):
				}
			}
			return true, encoder.Encode(map[string]any{"acknowledgement": ack, "readState": after, "verified": true})
		default:
			return true, fmt.Errorf("unknown Deribit order command %q", args[3])
		}
	case "reconcile":
		if len(args) != 3 {
			return true, errors.New("reconcile takes no arguments")
		}
		reconciler, err := newDeribitReconciler(ctx, cfg, client)
		if err != nil {
			return true, err
		}
		report, err := reconciler.Run(ctx)
		if err != nil {
			return true, err
		}
		return true, encoder.Encode(report)
	default:
		return true, fmt.Errorf("unknown Deribit command %q", args[2])
	}
}

func newDeribitReconciler(ctx context.Context, cfg config.Config, client *deribit.Client) (*deribitreconcile.Reconciler, error) {
	state, err := orderstate.NewService(ctx, orderstate.FileStore{Path: cfg.StateFile})
	if err != nil {
		return nil, err
	}
	return &deribitreconcile.Reconciler{Queries: client, Intents: intent.Store{Path: cfg.IntentFile}, State: state, AccountAlias: cfg.Deribit.AccountAlias}, nil
}

func decimalEqual(left, right string) bool {
	l, lok := new(big.Rat).SetString(left)
	r, rok := new(big.Rat).SetString(right)
	return lok && rok && l.Cmp(r) == 0
}

func splitNonempty(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("list contains an empty value")
		}
		result = append(result, part)
	}
	return result, nil
}

func deribitCurrencies(ctx context.Context, client *deribit.Client, requested string) ([]string, error) {
	if !strings.EqualFold(strings.TrimSpace(requested), "all") {
		parts := strings.Split(requested, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.ToUpper(strings.TrimSpace(part))
			if part == "" {
				return nil, errors.New("currency list contains an empty value")
			}
			result = append(result, part)
		}
		return result, nil
	}
	currencies, err := client.Currencies(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(currencies))
	for _, currency := range currencies {
		if currency.Currency != "" {
			result = append(result, currency.Currency)
		}
	}
	return result, nil
}
