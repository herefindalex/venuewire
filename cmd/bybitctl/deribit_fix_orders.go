package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bybit/internal/config"
	"bybit/internal/deribit"
	"bybit/internal/deribitfix"
	bybitfix "bybit/internal/fix"
	"bybit/internal/intent"
)

type deribitFIXRejectError struct {
	MessageType string
	ReasonCode  string
}

func (e *deribitFIXRejectError) Error() string {
	return fmt.Sprintf("Deribit FIX %s rejected request with reason code %s", e.MessageType, e.ReasonCode)
}

func requireDeribitFIXTradingGates(cfg config.Config) error {
	if !cfg.Deribit.FIXEnabled {
		return errors.New("DERIBIT_FIX_ENABLED must be true for a FIX write")
	}
	for _, gate := range []string{"RUN_MULTI_VENUE_E2E", "RUN_DERIBIT_TRADING_TESTS", "RUN_DERIBIT_FIX_TESTS"} {
		if strings.TrimSpace(os.Getenv(gate)) != "1" {
			return fmt.Errorf("Deribit FIX write requires %s=1", gate)
		}
	}
	return cfg.Deribit.RequireCredentials()
}

func placeDeribitFIX(ctx context.Context, cfg config.Config, client *deribit.Client, plan intent.Plan) (deribit.OrderResult, error) {
	if err := requireDeribitFIXTradingGates(cfg); err != nil {
		return deribit.OrderResult{}, err
	}
	evidence, err := runDeribitFIXWrite(ctx, cfg, client, fixWriteRequest{
		Instrument:          plan.Instrument,
		ExpectedClientID:    plan.ID,
		ExpectedLabel:       plan.ID,
		ExpectedAmountUnits: plan.Amount,
		Build: func(quantity deribitfix.QuantitySpec) (string, []bybitfix.Field, error) {
			fields, buildErr := deribitfix.NewOrderFields(deribitfix.NewOrderRequest{
				ClientClOrdID: plan.ID,
				Label:         plan.ID,
				Symbol:        plan.Instrument,
				Side:          plan.Side,
				Amount:        plan.Amount,
				Price:         plan.Price,
				TimeInForce:   plan.TimeInForce,
				PostOnly:      plan.PostOnly,
				ReduceOnly:    plan.ReduceOnly,
				Quantity:      quantity,
			})
			return "D", fields, buildErr
		},
	})
	if err != nil {
		return deribit.OrderResult{}, err
	}
	if evidence.NativeOrderID == "" {
		return deribit.OrderResult{}, &deribit.OutcomeUnknownError{Method: "FIX D", Cause: errors.New("ExecutionReport missing native OrderID(37)")}
	}
	if evidence.Status == "8" {
		return deribit.OrderResult{}, &deribitFIXRejectError{MessageType: "NewOrderSingle", ReasonCode: evidence.RejectReason}
	}
	return deribit.OrderResult{Order: deribit.Order{
		OrderID:        evidence.NativeOrderID,
		Label:          plan.ID,
		InstrumentName: plan.Instrument,
		Direction:      plan.Side,
		OrderType:      plan.OrderType,
		OrderState:     deribitOrderStateFromFIX(evidence.Status),
		Amount:         json.Number(plan.Amount),
		FilledAmount:   json.Number("0"),
		Price:          json.Number(plan.Price),
		ReduceOnly:     plan.ReduceOnly,
	}}, nil
}

func amendDeribitFIX(ctx context.Context, cfg config.Config, client *deribit.Client, before deribit.Order, ownedPlan intent.Plan, amount, price string) (deribitfix.OrderEvidence, error) {
	if err := requireDeribitFIXTradingGates(cfg); err != nil {
		return deribitfix.OrderEvidence{}, err
	}
	return runDeribitFIXWrite(ctx, cfg, client, fixWriteRequest{
		Instrument:          before.InstrumentName,
		ExpectedNativeID:    before.OrderID,
		ExpectedClientID:    before.Label,
		ExpectedLabel:       before.Label,
		ExpectedAmountUnits: amount,
		Build: func(quantity deribitfix.QuantitySpec) (string, []bybitfix.Field, error) {
			fields, buildErr := deribitfix.ReplaceOrderFields(deribitfix.ReplaceOrderRequest{
				ExistingOrderRef: deribitfix.ExistingOrderRef{Label: before.Label, Symbol: before.InstrumentName},
				Side:             before.Direction,
				Amount:           amount,
				Price:            price,
				PostOnly:         ownedPlan.PostOnly,
				ReduceOnly:       ownedPlan.ReduceOnly,
				Quantity:         quantity,
			})
			return "G", fields, buildErr
		},
	})
}

func requireConnectorOwnedFIXOrder(ctx context.Context, store intent.Store, order deribit.Order) (intent.Plan, error) {
	if order.Label == "" || order.OrderID == "" {
		return intent.Plan{}, errors.New("FIX amend/cancel requires independently read order ID and label")
	}
	plan, err := store.Get(ctx, order.Label)
	if err != nil {
		return intent.Plan{}, errors.New("FIX amend/cancel is restricted to a connector-owned persisted intent")
	}
	if plan.ID != order.Label || plan.NativeOrderID != order.OrderID || plan.Instrument != order.InstrumentName || plan.Transport != "fix" {
		return intent.Plan{}, errors.New("FIX order does not match its connector-owned persisted intent")
	}
	return plan, nil
}

func cancelDeribitFIX(ctx context.Context, cfg config.Config, client *deribit.Client, before deribit.Order) (deribitfix.OrderEvidence, error) {
	if err := requireDeribitFIXTradingGates(cfg); err != nil {
		return deribitfix.OrderEvidence{}, err
	}
	return runDeribitFIXWrite(ctx, cfg, client, fixWriteRequest{
		Instrument:          before.InstrumentName,
		ExpectedNativeID:    before.OrderID,
		ExpectedClientID:    before.Label,
		ExpectedLabel:       before.Label,
		ExpectedAmountUnits: before.Amount.String(),
		Build: func(deribitfix.QuantitySpec) (string, []bybitfix.Field, error) {
			fields, buildErr := deribitfix.CancelOrderFields(deribitfix.ExistingOrderRef{
				Label:  before.Label,
				Symbol: before.InstrumentName,
			})
			return "F", fields, buildErr
		},
	})
}

type fixWriteRequest struct {
	Instrument          string
	ExpectedNativeID    string
	ExpectedClientID    string
	ExpectedLabel       string
	ExpectedAmountUnits string
	Build               func(deribitfix.QuantitySpec) (string, []bybitfix.Field, error)
}

type securityResult struct {
	Spec deribitfix.QuantitySpec
	Err  error
}

func runDeribitFIXWrite(ctx context.Context, cfg config.Config, client *deribit.Client, request fixWriteRequest) (deribitfix.OrderEvidence, error) {
	instrument, err := client.Instrument(ctx, request.Instrument)
	if err != nil {
		return deribitfix.OrderEvidence{}, fmt.Errorf("read JSON-RPC instrument metadata before FIX write: %w", err)
	}
	host, _, err := net.SplitHostPort(cfg.Deribit.FIXAddress)
	if err != nil {
		return deribitfix.OrderEvidence{}, err
	}

	securityResults := make(chan securityResult, 1)
	reports := make(chan deribitfix.OrderEvidence, 4)
	rejects := make(chan error, 1)
	var securityOnce sync.Once
	var quantity atomic.Pointer[deribitfix.QuantitySpec]
	var writeAttempted atomic.Bool
	session, err := deribitfix.NewSession(deribitfix.SessionConfig{
		Dial: bybitfix.TLSDialer(cfg.Deribit.FIXAddress, host, 10*time.Second),
		Auth: &deribitfix.Authenticator{
			ClientID:                 cfg.Deribit.APIKey,
			ClientSecret:             cfg.Deribit.APISecret,
			SenderCompID:             cfg.Deribit.FIXSenderCompID,
			HeartbeatSeconds:         10,
			CancelOnDisconnect:       false,
			ReportFillsAsExecReports: true,
		},
		Heartbeat: 10 * time.Second,
		OnRecovery: func(recoveryCtx context.Context) error {
			if _, readErr := client.AccountSummaries(recoveryCtx); readErr != nil {
				return fmt.Errorf("post-FIX-recovery JSON-RPC reconciliation: %w", readErr)
			}
			return nil
		},
		OnMessage: func(_ context.Context, message bybitfix.Message) error {
			messageType, _ := message.Get(35)
			switch messageType {
			case "y":
				securities, parseErr := deribitfix.ParseSecurityList(message)
				securityOnce.Do(func() {
					if parseErr != nil {
						securityResults <- securityResult{Err: parseErr}
						return
					}
					for _, security := range securities {
						if security.Symbol != request.Instrument {
							continue
						}
						spec, specErr := deribitfix.QuantitySpecFromMetadata(instrument, security)
						securityResults <- securityResult{Spec: spec, Err: specErr}
						return
					}
					securityResults <- securityResult{Err: errors.New("FIX SecurityList omitted planned instrument")}
				})
				return nil
			case "8":
				spec := quantity.Load()
				if spec == nil {
					return errors.New("ExecutionReport arrived before quantity metadata validation")
				}
				evidence, parseErr := deribitfix.ParseExecutionReport(message, request.ExpectedClientID, request.ExpectedLabel, request.ExpectedAmountUnits, *spec)
				if parseErr != nil {
					return parseErr
				}
				if request.ExpectedNativeID != "" {
					if evidence.NativeOrderID != "" && evidence.NativeOrderID != request.ExpectedNativeID {
						return errors.New("ExecutionReport native OrderID conflicts with independent JSON pre-read")
					}
					if evidence.NativeOrderID == "" {
						evidence.NativeOrderID = request.ExpectedNativeID
					}
				}
				select {
				case reports <- evidence:
					return nil
				default:
					return errors.New("Deribit FIX ExecutionReport queue full")
				}
			case "9":
				evidence, parseErr := deribitfix.ParseOrderCancelReject(message)
				if parseErr != nil {
					return parseErr
				}
				select {
				case rejects <- &deribitFIXRejectError{MessageType: "cancel/replace", ReasonCode: evidence.ReasonCode}:
					return nil
				default:
					return errors.New("Deribit FIX reject queue full")
				}
			default:
				return nil
			}
		},
	})
	if err != nil {
		return deribitfix.OrderEvidence{}, err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- session.Run(sessionCtx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}()
	if err := waitForDeribitFIX(ctx, 15*time.Second, done, nil, func() bool { return session.Stats().LoggedOn }); err != nil {
		return deribitfix.OrderEvidence{}, err
	}
	securityRequestID := fmt.Sprintf("sec-%d", time.Now().UTC().UnixMilli())
	securityFields, err := deribitfix.SecurityListRequestFields(securityRequestID, instrument.BaseCurrency)
	if err != nil {
		return deribitfix.OrderEvidence{}, err
	}
	if err := session.Send("x", securityFields); err != nil {
		return deribitfix.OrderEvidence{}, err
	}
	var validated deribitfix.QuantitySpec
	select {
	case result := <-securityResults:
		if result.Err != nil {
			return deribitfix.OrderEvidence{}, result.Err
		}
		validated = result.Spec
	case runErr := <-done:
		return deribitfix.OrderEvidence{}, fmt.Errorf("Deribit FIX ended before SecurityList validation: %w", runErr)
	case <-ctx.Done():
		return deribitfix.OrderEvidence{}, ctx.Err()
	case <-time.After(15 * time.Second):
		return deribitfix.OrderEvidence{}, errors.New("timed out waiting for Deribit FIX SecurityList")
	}
	quantity.Store(&validated)
	messageType, fields, err := request.Build(validated)
	if err != nil {
		return deribitfix.OrderEvidence{}, err
	}
	writeAttempted.Store(true)
	if err := session.Send(messageType, fields); err != nil {
		return deribitfix.OrderEvidence{}, &deribit.OutcomeUnknownError{Method: "FIX " + messageType, Cause: err}
	}
	select {
	case report := <-reports:
		if (messageType == "D" || messageType == "G") && report.FIXOrderContracts == "" {
			return report, &deribit.OutcomeUnknownError{Method: "FIX " + messageType, Cause: errors.New("ExecutionReport missing required OrderQty(38)")}
		}
		if report.Status == "8" {
			return report, &deribitFIXRejectError{MessageType: messageType, ReasonCode: report.RejectReason}
		}
		return report, nil
	case reject := <-rejects:
		return deribitfix.OrderEvidence{}, reject
	case runErr := <-done:
		if writeAttempted.Load() {
			return deribitfix.OrderEvidence{}, &deribit.OutcomeUnknownError{Method: "FIX " + messageType, Cause: runErr}
		}
		return deribitfix.OrderEvidence{}, runErr
	case <-ctx.Done():
		return deribitfix.OrderEvidence{}, &deribit.OutcomeUnknownError{Method: "FIX " + messageType, Cause: ctx.Err()}
	case <-time.After(15 * time.Second):
		return deribitfix.OrderEvidence{}, &deribit.OutcomeUnknownError{Method: "FIX " + messageType, Cause: errors.New("timed out waiting for independently correlated ExecutionReport")}
	}
}

func deribitOrderStateFromFIX(status string) string {
	switch status {
	case "0", "1":
		return "open"
	case "2":
		return "filled"
	case "4":
		return "cancelled"
	case "8":
		return "rejected"
	default:
		return "unknown"
	}
}
