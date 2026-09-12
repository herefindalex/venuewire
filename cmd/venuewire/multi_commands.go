package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/herefindalex/venuewire/internal/config"
	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/rest"
)

type venueResult struct {
	Venue        string `json:"venue"`
	Environment  string `json:"environment"`
	AccountAlias string `json:"accountAlias"`
	OK           bool   `json:"ok"`
	Data         any    `json:"data,omitempty"`
	Error        string `json:"error,omitempty"`
}

func executeMultiVenueCommand(ctx context.Context, cfg config.Config, args []string, output io.Writer) (bool, error) {
	if len(args) < 3 || args[0] != "venue" || args[1] != "all" {
		return false, nil
	}
	if args[2] != "status" && args[2] != "portfolio" {
		return true, errors.New("--venue all supports status or portfolio")
	}
	results := make([]venueResult, 0, 2)
	bybit := venueResult{Venue: "bybit", Environment: "testnet", AccountAlias: cfg.AccountAlias}
	if err := cfg.RequireRESTCredentials(); err != nil {
		bybit.Error = err.Error()
	} else {
		client := rest.NewClient(cfg.RESTBaseURL, cfg.APIKey, cfg.APISecret)
		if args[2] == "status" {
			value, _, err := client.ServerTime(ctx)
			if err != nil {
				bybit.Error = err.Error()
			} else {
				bybit.OK = true
				bybit.Data = value
			}
		} else {
			value, _, err := client.WalletBalances(ctx, "")
			if err != nil {
				bybit.Error = err.Error()
			} else {
				bybit.OK = true
				bybit.Data = value
			}
		}
	}
	results = append(results, bybit)
	dr := venueResult{Venue: "deribit", Environment: "testnet", AccountAlias: cfg.Deribit.AccountAlias}
	if !cfg.Deribit.Enabled {
		dr.Error = "DERIBIT_ENABLED must be true"
	} else if err := cfg.Deribit.RequireCredentials(); err != nil {
		dr.Error = err.Error()
	} else {
		client, err := deribit.NewClient(cfg.Deribit.HTTPBaseURL, cfg.Deribit.APIKey, cfg.Deribit.APISecret, nil)
		if err != nil {
			dr.Error = err.Error()
		} else if args[2] == "status" {
			value, err := client.ServerTime(ctx)
			if err != nil {
				dr.Error = err.Error()
			} else {
				dr.OK = true
				dr.Data = map[string]any{"timeMs": value, "time": time.UnixMilli(value).UTC()}
			}
		} else {
			value, err := client.AccountSummaries(ctx)
			if err != nil {
				dr.Error = err.Error()
			} else {
				dr.OK = true
				dr.Data = value
			}
		}
	}
	results = append(results, dr)
	return true, json.NewEncoder(output).Encode(map[string]any{"results": results, "allHealthy": bybit.OK && dr.OK})
}
