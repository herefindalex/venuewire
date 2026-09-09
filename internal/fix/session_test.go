package fix

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

type fixtureSigner struct{}

func (fixtureSigner) Sign(payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", errors.New("empty payload")
	}
	return "fixture-base64-signature", nil
}

func TestSessionLogonTestRequestHeartbeatAndLogout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := fixtureSession(t, func(context.Context) (Transport, error) { return clientConn, nil })
	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		logon, err := readFIX(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if msgType(logon) != "A" || mustTag(logon, 34) != "1" || mustTag(logon, 141) != "Y" || mustTag(logon, 95) != strconv.Itoa(len("fixture-base64-signature")) {
			serverDone <- errors.New("invalid Logon fields")
			return
		}
		if err := sendServer(serverConn, 1, "A", nil); err != nil {
			serverDone <- err
			return
		}
		if err := sendServer(serverConn, 2, "1", []Field{{112, "server-test-1"}}); err != nil {
			serverDone <- err
			return
		}
		for {
			message, err := readFIX(serverConn)
			if err != nil {
				serverDone <- err
				return
			}
			if msgType(message) == "0" && mustTag(message, 112) == "server-test-1" {
				break
			}
		}
		cancel()
		logout, err := readFIX(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if msgType(logout) != "5" {
			serverDone <- errors.New("client did not send Logout")
			return
		}
		serverDone <- nil
	}()
	if err := session.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	stats := session.Stats()
	if stats.Inbound < 2 || stats.Outbound < 3 || stats.TestRequests == 0 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestSessionSendsHeartbeatOnlyAfterOutboundIdle(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	session := fixtureSession(t, func(context.Context) (Transport, error) { return clientConn, nil })
	done := make(chan error, 1)
	go func() { done <- session.RunOnce(ctx) }()
	if _, err := readFIX(serverConn); err != nil {
		t.Fatal(err)
	}
	if err := sendServer(serverConn, 1, "A", nil); err != nil {
		t.Fatal(err)
	}
	message, err := readFIX(serverConn)
	if err != nil {
		t.Fatal(err)
	}
	if msgType(message) != "0" {
		t.Fatalf("expected idle Heartbeat, got %s", msgType(message))
	}
	cancel()
	_, _ = readFIX(serverConn)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type cancellationErrorTransport struct {
	ctx context.Context
	bytes.Buffer
}

func (t *cancellationErrorTransport) Read([]byte) (int, error) {
	<-t.ctx.Done()
	return 0, t.ctx.Err()
}

func (*cancellationErrorTransport) Close() error { return nil }

func TestSessionTreatsReaderCancellationAsCleanShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	transport := &cancellationErrorTransport{ctx: ctx}
	session := fixtureSession(t, func(context.Context) (Transport, error) { return transport, nil })
	done := make(chan error, 1)
	go func() { done <- session.RunOnce(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancellation returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session did not stop after cancellation")
	}
}

func TestApplicationTrafficResetsHeartbeatIdleTimer(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	session := fixtureSession(t, func(context.Context) (Transport, error) { return clientConn, nil })
	done := make(chan error, 1)
	go func() { done <- session.RunOnce(ctx) }()
	if _, err := readFIX(serverConn); err != nil {
		t.Fatal(err)
	}
	if err := sendServer(serverConn, 1, "A", nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
	sendDone := make(chan error, 1)
	go func() { sendDone <- session.SendApplication("D", []Field{{55, "BTCUSDT"}}) }()
	if message, err := readFIX(serverConn); err != nil || msgType(message) != "D" {
		t.Fatalf("application message=%s err=%v", msgType(message), err)
	}
	if err := <-sendDone; err != nil {
		t.Fatal(err)
	}
	if err := serverConn.SetReadDeadline(time.Now().Add(25 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := readFIX(serverConn); err == nil {
		t.Fatal("heartbeat sent before application idle interval")
	}
	if err := serverConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	message, err := readFIX(serverConn)
	if err != nil {
		t.Fatal(err)
	}
	if msgType(message) != "0" {
		t.Fatalf("expected heartbeat, got %s", msgType(message))
	}
	_ = serverConn.SetReadDeadline(time.Time{})
	cancel()
	_, _ = readFIX(serverConn)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSessionSequenceAnomaliesAreExplicit(t *testing.T) {
	for _, tt := range []struct {
		name          string
		first, second int64
	}{{"higher", 2, -1}, {"lower_duplicate", 1, 1}} {
		t.Run(tt.name, func(t *testing.T) {
			clientConn, serverConn := net.Pipe()
			defer serverConn.Close()
			session := fixtureSession(t, func(context.Context) (Transport, error) { return clientConn, nil })
			go func() {
				_, _ = readFIX(serverConn)
				_ = sendServer(serverConn, tt.first, "A", nil)
				if tt.second > 0 {
					_ = sendServer(serverConn, tt.second, "0", nil)
				}
			}()
			err := session.RunOnce(context.Background())
			var sequenceErr *SequenceError
			if !errors.As(err, &sequenceErr) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestAccessDeniedLogoutHasDistinctError(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	session := fixtureSession(t, func(context.Context) (Transport, error) { return clientConn, nil })
	go func() {
		_, _ = readFIX(serverConn)
		_ = sendServer(serverConn, 1, "5", []Field{{58, "access denied: whitelist required"}})
	}()
	err := session.RunOnce(context.Background())
	var denied *AccessDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error=%v", err)
	}
}

func TestMalformedLogonResponseIsRejected(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	session := fixtureSession(t, func(context.Context) (Transport, error) { return clientConn, nil })
	go func() {
		_, _ = readFIX(serverConn)
		_, _ = serverConn.Write([]byte("8=FIX.4.4\x019=5\x0135=A\x0110=000\x01"))
	}()
	if err := session.RunOnce(context.Background()); err == nil {
		t.Fatal("malformed Logon response accepted")
	}
}

func fixtureSession(t *testing.T, dial DialFunc) *Session {
	t.Helper()
	session, err := NewSession(SessionConfig{Dial: dial, SenderCompID: "SENDER", APIKey: "fixture-key", Signer: fixtureSigner{}, Heartbeat: 40 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func sendServer(writer io.Writer, sequence int64, messageType string, body []Field) error {
	fields := []Field{{35, messageType}, {49, "BYBIT_FIX_SERVER"}, {56, "SENDER"}, {34, strconv.FormatInt(sequence, 10)}, {52, "20260909-14:30:00.000"}}
	fields = append(fields, body...)
	encoded, err := Encode("FIX.4.4", fields)
	if err != nil {
		return err
	}
	return writeAll(writer, encoded)
}

func readFIX(reader io.Reader) (Message, error) {
	framer := &Framer{}
	buffer := make([]byte, 256)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			frames, frameErr := framer.Feed(buffer[:count])
			if frameErr != nil {
				return Message{}, frameErr
			}
			if len(frames) > 0 {
				return ParseStrict(frames[0])
			}
		}
		if err != nil {
			return Message{}, err
		}
	}
}

func msgType(message Message) string          { value, _ := message.Get(35); return value }
func mustTag(message Message, tag int) string { value, _ := message.Get(tag); return value }
