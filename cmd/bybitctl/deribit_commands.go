package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"bybit/internal/config"
	"bybit/internal/deribit"
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
	default:
		return true, fmt.Errorf("unknown Deribit command %q", args[2])
	}
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
