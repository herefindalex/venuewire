package deribitfixmock

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/herefindalex/venuewire/internal/deribitfix"
	bybitfix "github.com/herefindalex/venuewire/internal/fix"
)

func TestServerExercisesDeribitSessionDialect(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := Server{ClientID: "fixture-id", ClientSecret: "fixture-secret"}
	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		serverDone <- server.Serve(ctx, serverConn)
	}()
	auth := &deribitfix.Authenticator{
		ClientID:                 "fixture-id",
		ClientSecret:             "fixture-secret",
		SenderCompID:             "fixture-sender",
		HeartbeatSeconds:         10,
		CancelOnDisconnect:       false,
		ReportFillsAsExecReports: true,
	}
	session, err := deribitfix.NewSession(deribitfix.SessionConfig{
		Dial:       func(context.Context) (bybitfix.Transport, error) { return clientConn, nil },
		Auth:       auth,
		Heartbeat:  time.Hour,
		OnRecovery: func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	clientDone := make(chan error, 1)
	go func() { clientDone <- session.Run(ctx) }()

	deadline := time.After(3 * time.Second)
	for {
		stats := session.Stats()
		if stats.LoggedOn && stats.TestRequests == 1 && stats.ResendRequests == 1 && stats.SequenceResets == 1 && stats.Inbound >= 5 {
			break
		}
		select {
		case err := <-clientDone:
			t.Fatalf("client ended before handshake completed: %v", err)
		case err := <-serverDone:
			t.Fatalf("server ended before Logout: %v", err)
		case <-deadline:
			t.Fatalf("timed out waiting for session evidence: %+v", stats)
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-clientDone; err != nil {
		t.Fatalf("client error: %v", err)
	}
	if err := <-serverDone; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("server error: %v", err)
	}
}

func TestServerRejectsWrongSecretWithoutEchoingSensitiveData(t *testing.T) {
	auth := &deribitfix.Authenticator{
		ClientID:                 "fixture-id",
		ClientSecret:             "wrong-secret",
		SenderCompID:             "fixture-sender",
		HeartbeatSeconds:         10,
		ReportFillsAsExecReports: true,
		Now:                      func() time.Time { return time.UnixMilli(1788960000123) },
		Random:                   bytes.NewReader(bytes.Repeat([]byte{3}, 32)),
	}
	raw, err := auth.Logon(1)
	if err != nil {
		t.Fatal(err)
	}
	message, err := bybitfix.ParseStrict(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Server{ClientID: "fixture-id", ClientSecret: "real-secret"}).validateLogon(message)
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("error=%T %v, want AuthenticationError", err, err)
	}
	for _, sensitive := range []string{"wrong-secret", "real-secret"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("authentication error exposed secret: %v", err)
		}
	}
}
