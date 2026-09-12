package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/herefindalex/venuewire/internal/config"
	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/deribitfix"
	"github.com/herefindalex/venuewire/internal/deribitfixmock"
	bybitfix "github.com/herefindalex/venuewire/internal/fix"
)

func executeDeribitFIXCommand(ctx context.Context, cfg config.Config, logger *slog.Logger, args []string, output io.Writer) (bool, error) {
	if len(args) < 3 || args[0] != "venue" || args[1] != "deribit" || args[2] != "fix" {
		return false, nil
	}
	if len(args) < 4 {
		return true, errors.New("Deribit FIX command requires mock-demo or connect-testnet")
	}
	switch args[3] {
	case "mock-demo":
		if len(args) != 4 {
			return true, errors.New("Deribit FIX mock-demo takes no arguments")
		}
		return true, runDeribitFIXMockDemo(ctx, cfg, logger, output)
	case "connect-testnet":
		return true, runDeribitFIXTestnet(ctx, cfg, logger, args[4:], output)
	default:
		return true, fmt.Errorf("unknown Deribit FIX command %q", args[3])
	}
}

func runDeribitFIXMockDemo(ctx context.Context, cfg config.Config, logger *slog.Logger, output io.Writer) error {
	clientConn, serverConn := net.Pipe()
	demoCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		serverDone <- (deribitfixmock.Server{
			ClientID:       "local-fixture",
			ClientSecret:   "local-fixture-secret",
			OrderLifecycle: true,
		}).Serve(demoCtx, serverConn)
	}()

	auth := &deribitfix.Authenticator{
		ClientID:                 "local-fixture",
		ClientSecret:             "local-fixture-secret",
		SenderCompID:             "local-fixture-sender",
		HeartbeatSeconds:         10,
		CancelOnDisconnect:       false,
		ReportFillsAsExecReports: true,
	}
	var reconciled atomic.Bool
	securityReady := make(chan deribitfix.QuantitySpec, 1)
	reports := make(chan deribitfix.OrderEvidence, 4)
	session, err := deribitfix.NewSession(deribitfix.SessionConfig{
		Dial:      func(context.Context) (bybitfix.Transport, error) { return clientConn, nil },
		Auth:      auth,
		Heartbeat: time.Hour,
		OnRecovery: func(context.Context) error {
			reconciled.Store(true)
			return nil
		},
		OnMessage: func(_ context.Context, message bybitfix.Message) error {
			messageType, _ := message.Get(35)
			switch messageType {
			case "y":
				securities, parseErr := deribitfix.ParseSecurityList(message)
				if parseErr != nil {
					return parseErr
				}
				for _, security := range securities {
					if security.Symbol != "BTC-PERPETUAL" {
						continue
					}
					spec, specErr := deribitfix.QuantitySpecFromMetadata(deribit.Instrument{
						InstrumentName:     "BTC-PERPETUAL",
						Kind:               "future",
						SettlementCurrency: "BTC",
						ContractSize:       json.Number("10"),
						MinTradeAmount:     json.Number("10"),
					}, security)
					if specErr != nil {
						return specErr
					}
					select {
					case securityReady <- spec:
					default:
						return errors.New("duplicate mock SecurityList")
					}
					return nil
				}
				return errors.New("mock SecurityList omitted BTC-PERPETUAL")
			case "8":
				quantity := deribitfix.QuantitySpec{
					Symbol:             "BTC-PERPETUAL",
					SettlementCurrency: "BTC",
					JSONAmountUnit:     "USD_notional",
					FIXQtyType:         deribitfix.QtyTypeUnits,
					ContractMultiplier: "10",
					MinTradeAmount:     "10",
					MinTradeContracts:  "1",
				}
				evidence, parseErr := deribitfix.ParseExecutionReport(message, "mock-intent-1", "mock-intent-1", "10", quantity)
				if parseErr != nil {
					return parseErr
				}
				select {
				case reports <- evidence:
					return nil
				default:
					return errors.New("mock execution-report queue full")
				}
			default:
				return fmt.Errorf("unexpected mock Deribit FIX application MsgType=%q", messageType)
			}
		},
	})
	if err != nil {
		return err
	}
	clientDone := make(chan error, 1)
	go func() { clientDone <- session.Run(demoCtx) }()

	err = waitForDeribitFIX(ctx, 3*time.Second, clientDone, serverDone, func() bool {
		stats := session.Stats()
		return stats.LoggedOn && stats.TestRequests == 1 && stats.ResendRequests == 1 && stats.SequenceResets == 1 && stats.Inbound >= 5
	})
	if err != nil {
		cancel()
		return err
	}
	securityRequest, err := deribitfix.SecurityListRequestFields("mock-security", "BTC")
	if err != nil {
		cancel()
		return err
	}
	if err := session.Send("x", securityRequest); err != nil {
		cancel()
		return err
	}
	var quantity deribitfix.QuantitySpec
	select {
	case quantity = <-securityReady:
	case err := <-clientDone:
		cancel()
		return fmt.Errorf("Deribit FIX mock ended before SecurityList: %w", err)
	case err := <-serverDone:
		cancel()
		return fmt.Errorf("Deribit FIX mock server ended before SecurityList: %w", err)
	case <-time.After(3 * time.Second):
		cancel()
		return errors.New("timed out waiting for Deribit FIX SecurityList")
	}
	newFields, err := deribitfix.NewOrderFields(deribitfix.NewOrderRequest{
		ClientClOrdID: "mock-intent-1",
		Label:         "mock-intent-1",
		Symbol:        "BTC-PERPETUAL",
		Side:          "buy",
		Amount:        "10",
		Price:         "80000",
		TimeInForce:   "gtc",
		PostOnly:      true,
		Quantity:      quantity,
	})
	if err != nil {
		cancel()
		return err
	}
	if err := session.Send("D", newFields); err != nil {
		cancel()
		return err
	}
	accepted, err := waitDeribitFIXReport(ctx, reports, clientDone, "0")
	if err != nil {
		cancel()
		return err
	}
	replaceFields, err := deribitfix.ReplaceOrderFields(deribitfix.ReplaceOrderRequest{
		ExistingOrderRef: deribitfix.ExistingOrderRef{ServerClOrdID: accepted.ServerClOrdID, Symbol: "BTC-PERPETUAL"},
		Side:             "buy",
		Amount:           "10",
		Price:            "79990",
		PostOnly:         true,
		Quantity:         quantity,
	})
	if err != nil {
		cancel()
		return err
	}
	if err := session.Send("G", replaceFields); err != nil {
		cancel()
		return err
	}
	replaced, err := waitDeribitFIXReport(ctx, reports, clientDone, "0")
	if err != nil {
		cancel()
		return err
	}
	cancelFields, err := deribitfix.CancelOrderFields(deribitfix.ExistingOrderRef{ServerClOrdID: replaced.ServerClOrdID, Symbol: "BTC-PERPETUAL"})
	if err != nil {
		cancel()
		return err
	}
	if err := session.Send("F", cancelFields); err != nil {
		cancel()
		return err
	}
	cancelled, err := waitDeribitFIXReport(ctx, reports, clientDone, "4")
	if err != nil {
		cancel()
		return err
	}
	stats := session.Stats()
	cancel()
	if err := waitDeribitFIXShutdown(clientDone, serverDone); err != nil {
		return err
	}
	if !reconciled.Load() {
		return errors.New("Deribit FIX mock did not invoke recovery reconciliation")
	}
	logger.Info("Deribit FIX local mock complete",
		slog.String("venue", "deribit"),
		slog.String("environment", "testnet"),
		slog.String("transport", "fix"),
		slog.String("validation_level", "LOCAL_TESTED"),
	)
	return writeJSON(output, map[string]any{
		"venue":                    "deribit",
		"environment":              "testnet",
		"accountAlias":             cfg.Deribit.AccountAlias,
		"transport":                "fix",
		"targetCompID":             deribitfix.TargetCompID,
		"validationLevel":          "LOCAL_TESTED",
		"authenticated":            true,
		"heartbeatTestRequest":     stats.TestRequests == 1,
		"resendSequenceReset":      stats.ResendRequests == 1 && stats.SequenceResets == 1,
		"reconciliationCallback":   reconciled.Load(),
		"securityListMultiplier":   quantity.ContractMultiplier,
		"mockNativeOrderId":        cancelled.NativeOrderID,
		"mockFinalOrderStatus":     cancelled.Status,
		"mockLifecycle":            []string{"NewOrderSingle(D)", "OrderCancelReplaceRequest(G)", "OrderCancelRequest(F)", "ExecutionReport(8)"},
		"tradingWritePerformed":    false,
		"recoveryBufferedAtFinish": stats.RecoveryBuffered,
	})
}

func waitDeribitFIXReport(ctx context.Context, reports <-chan deribitfix.OrderEvidence, clientDone <-chan error, wantStatus string) (deribitfix.OrderEvidence, error) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return deribitfix.OrderEvidence{}, ctx.Err()
	case err := <-clientDone:
		if err == nil {
			return deribitfix.OrderEvidence{}, errors.New("Deribit FIX client ended before ExecutionReport")
		}
		return deribitfix.OrderEvidence{}, err
	case report := <-reports:
		if report.Status != wantStatus {
			return deribitfix.OrderEvidence{}, fmt.Errorf("Deribit FIX OrdStatus=%q, want %q", report.Status, wantStatus)
		}
		return report, nil
	case <-timer.C:
		return deribitfix.OrderEvidence{}, errors.New("timed out waiting for Deribit FIX ExecutionReport")
	}
}

func runDeribitFIXTestnet(ctx context.Context, cfg config.Config, logger *slog.Logger, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("venue deribit fix connect-testnet", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	duration := flags.Duration("duration", 5*time.Second, "time to keep the authenticated session open")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *duration <= 0 || *duration > 30*time.Second {
		return errors.New("duration must be greater than zero and at most 30s")
	}
	if !cfg.Deribit.FIXEnabled {
		return errors.New("DERIBIT_FIX_ENABLED must be true")
	}
	if strings.TrimSpace(os.Getenv("RUN_DERIBIT_FIX_TESTS")) != "1" {
		return errors.New("live Deribit FIX validation requires RUN_DERIBIT_FIX_TESTS=1")
	}
	if err := cfg.Deribit.RequireCredentials(); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(cfg.Deribit.FIXAddress)
	if err != nil {
		return err
	}
	client, err := deribit.NewClient(cfg.Deribit.HTTPBaseURL, cfg.Deribit.APIKey, cfg.Deribit.APISecret, nil)
	if err != nil {
		return err
	}
	auth := &deribitfix.Authenticator{
		ClientID:                 cfg.Deribit.APIKey,
		ClientSecret:             cfg.Deribit.APISecret,
		SenderCompID:             cfg.Deribit.FIXSenderCompID,
		HeartbeatSeconds:         10,
		CancelOnDisconnect:       false,
		ReportFillsAsExecReports: true,
	}
	var recoveryReads atomic.Uint64
	session, err := deribitfix.NewSession(deribitfix.SessionConfig{
		Dial:      bybitfix.TLSDialer(cfg.Deribit.FIXAddress, host, 10*time.Second),
		Auth:      auth,
		Heartbeat: 10 * time.Second,
		OnRecovery: func(recoveryCtx context.Context) error {
			if _, readErr := client.AccountSummaries(recoveryCtx); readErr != nil {
				return fmt.Errorf("post-FIX-recovery JSON-RPC reconciliation: %w", readErr)
			}
			recoveryReads.Add(1)
			return nil
		},
	})
	if err != nil {
		return err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.Run(sessionCtx) }()
	if err := waitForDeribitFIX(ctx, 15*time.Second, done, nil, func() bool { return session.Stats().LoggedOn }); err != nil {
		cancel()
		return err
	}
	timer := time.NewTimer(*duration)
	select {
	case <-ctx.Done():
		timer.Stop()
		cancel()
		return ctx.Err()
	case err := <-done:
		timer.Stop()
		if err != nil {
			return err
		}
		return errors.New("Deribit FIX session ended before validation duration")
	case <-timer.C:
	}
	stats := session.Stats()
	cancel()
	if err := <-done; err != nil {
		return err
	}
	logger.Info("Deribit FIX Testnet Logon validated",
		slog.String("venue", "deribit"),
		slog.String("environment", "testnet"),
		slog.String("account_alias", cfg.Deribit.AccountAlias),
		slog.String("transport", "fix"),
		slog.String("validation_level", "TESTNET_LOGON"),
	)
	return writeJSON(output, map[string]any{
		"venue":                 "deribit",
		"environment":           "testnet",
		"accountAlias":          cfg.Deribit.AccountAlias,
		"transport":             "fix",
		"targetCompID":          deribitfix.TargetCompID,
		"validationLevel":       "TESTNET_LOGON",
		"authenticated":         stats.LoggedOn,
		"inboundMessages":       stats.Inbound,
		"outboundMessages":      stats.Outbound,
		"sequenceErrors":        stats.SequenceErrors,
		"recoveryJSONRPCReads":  recoveryReads.Load(),
		"tradingWritePerformed": false,
	})
}

func waitForDeribitFIX(ctx context.Context, timeout time.Duration, clientDone, serverDone <-chan error, ready func() bool) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if ready() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-clientDone:
			if err == nil {
				return errors.New("Deribit FIX client ended before expected state")
			}
			return err
		case err := <-serverDone:
			if serverDone == nil {
				continue
			}
			if err == nil {
				return errors.New("Deribit FIX mock ended before expected state")
			}
			return err
		case <-timer.C:
			return errors.New("timed out waiting for Deribit FIX session state")
		case <-ticker.C:
		}
	}
}

func waitDeribitFIXShutdown(clientDone, serverDone <-chan error) error {
	for clientDone != nil || serverDone != nil {
		select {
		case err := <-clientDone:
			clientDone = nil
			if err != nil {
				return err
			}
		case err := <-serverDone:
			serverDone = nil
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		case <-time.After(3 * time.Second):
			return errors.New("timed out shutting down Deribit FIX mock")
		}
	}
	return nil
}
