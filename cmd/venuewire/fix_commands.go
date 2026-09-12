package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/herefindalex/venuewire/internal/config"
	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/fix"
	"github.com/herefindalex/venuewire/internal/fixmock"
	"github.com/herefindalex/venuewire/internal/orderstate"
)

func executeFIXCommand(ctx context.Context, cfg config.Config, logger *slog.Logger, args []string, output io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "fix" {
		return false, nil
	}
	if len(args) < 2 {
		return true, errors.New("fix command requires mock-demo, mock-server, or connect-testnet")
	}
	switch args[1] {
	case "mock-demo":
		return true, runFIXMockDemo(ctx, cfg, logger, output)
	case "mock-server":
		return true, runFIXMockServer(ctx, logger, args[2:])
	case "connect-testnet":
		return true, runFIXTestnet(ctx, cfg, logger, output)
	default:
		return true, errors.New("unknown FIX command " + args[1])
	}
}

type demoSigner struct{}

func (demoSigner) Sign([]byte) (string, error) { return "local-mock-signature", nil }

func runFIXMockDemo(ctx context.Context, _ config.Config, logger *slog.Logger, output io.Writer) error {
	demoContext, cancel := context.WithCancel(ctx)
	defer cancel()
	dial, mockDone := fixmock.PairDialer(demoContext, fixmock.PartialThenFilled)
	tempDir, err := os.MkdirTemp("", "venuewire-fix-mock-")
	if err != nil {
		return fmt.Errorf("create isolated FIX mock state: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	state, err := orderstate.NewService(demoContext, orderstate.FileStore{Path: filepath.Join(tempDir, "orders.json")})
	if err != nil {
		return err
	}
	var router *fix.OrderRouter
	session, err := fix.NewSession(fix.SessionConfig{Dial: dial, SenderCompID: "FIX_CLIENT", APIKey: "local-fixture", Signer: demoSigner{}, Heartbeat: time.Second, OnMessage: func(messageContext context.Context, message fix.Message) error {
		return router.HandleMessage(messageContext, message)
	}})
	if err != nil {
		return err
	}
	router = &fix.OrderRouter{Session: session, State: state}
	done := make(chan error, 1)
	go func() { done <- session.RunOnce(demoContext) }()
	if err := waitUntil(demoContext, 2*time.Second, func() bool { return session.Stats().Inbound >= 1 }); err != nil {
		return err
	}
	linkID := "mock-" + time.Now().UTC().Format("20060102T150405")
	if err := router.Place(demoContext, fix.NewOrderRequest{Symbol: "BTCUSDT", Side: "Buy", OrderType: "Limit", Qty: "1.0", Price: "100", ClOrdID: linkID, TimeInForce: "GTC"}); err != nil {
		return err
	}
	if err := waitUntil(demoContext, 2*time.Second, func() bool { return state.Snapshot().Orders[linkID].Status == domain.OrderStatusFilled }); err != nil {
		return err
	}
	cancel()
	if err := <-done; err != nil {
		return fmt.Errorf("local FIX client shutdown: %w", err)
	}
	if mockErr := <-mockDone; mockErr != nil && !errors.Is(mockErr, context.Canceled) && !errors.Is(mockErr, io.EOF) {
		return fmt.Errorf("local FIX mock shutdown: %w", mockErr)
	}
	logger.Info("local FIX mock demo complete", slog.String("protocol", "FIX"), slog.String("orderLinkId", linkID))
	return writeJSON(output, state.Snapshot())
}

func runFIXMockServer(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := newFlagSet("fix mock-server")
	listen := flags.String("listen", "127.0.0.1:9001", "local test listen address")
	scenario := flags.String("scenario", string(fixmock.Accepted), "mock scenario")
	if err := flags.Parse(args); err != nil {
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() { <-ctx.Done(); _ = listener.Close() }()
	logger.Warn("local FIX mock listening without TLS for loopback diagnostics; integration tests use injected transport", slog.String("listen", *listen))
	for {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return acceptErr
		}
		go func(connection net.Conn) {
			if serveErr := fixmock.New(fixmock.Scenario(*scenario)).Serve(ctx, connection); serveErr != nil && !errors.Is(serveErr, io.EOF) {
				logger.Error("FIX mock connection failed", slog.String("error", serveErr.Error()))
			}
		}(conn)
	}
}

func runFIXTestnet(ctx context.Context, cfg config.Config, logger *slog.Logger, output io.Writer) error {
	signer, err := fix.LoadRSASigner(cfg.FIXPrivateKeyPath)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(cfg.FIXAddress)
	if err != nil {
		return err
	}
	session, err := fix.NewSession(fix.SessionConfig{Dial: fix.TLSDialer(cfg.FIXAddress, host, 10*time.Second), SenderCompID: "FIX_CLIENT", APIKey: cfg.FIXAPIKey, Signer: signer, OnMessage: func(_ context.Context, message fix.Message) error {
		messageType, _ := message.Get(35)
		logger.Info("FIX application message", slog.String("protocol", "FIX"), slog.String("msg_type", messageType), slog.String("sequence", valueForCLI(message, 34)))
		return writeJSON(output, map[string]string{"msgType": messageType, "sequence": valueForCLI(message, 34)})
	}})
	if err != nil {
		return err
	}
	return session.Run(ctx)
}
func valueForCLI(message fix.Message, tag int) string { value, _ := message.Get(tag); return value }
func waitUntil(ctx context.Context, timeout time.Duration, condition func() bool) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("timed out waiting for FIX mock state")
		case <-ticker.C:
		}
	}
}
