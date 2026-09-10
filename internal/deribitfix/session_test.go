package deribitfix

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	bybitfix "venuewire/internal/fix"
)

func encodeServer(t *testing.T, msgType string, seq int, body ...bybitfix.Field) []byte {
	t.Helper()
	fields := []bybitfix.Field{{Tag: 35, Value: msgType}, {Tag: 49, Value: TargetCompID}, {Tag: 56, Value: "client"}, {Tag: 34, Value: strconv.Itoa(seq)}, {Tag: 52, Value: "20260909-16:00:00.000"}}
	fields = append(fields, body...)
	raw, err := bybitfix.Encode("FIX.4.4", fields)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func readMessage(t *testing.T, reader io.Reader) bybitfix.Message {
	t.Helper()
	framer := bybitfix.Framer{}
	buffer := make([]byte, 4096)
	for {
		n, err := reader.Read(buffer)
		if err != nil {
			t.Fatal(err)
		}
		frames, err := framer.Feed(buffer[:n])
		if err != nil {
			t.Fatal(err)
		}
		if len(frames) > 0 {
			message, err := bybitfix.ParseStrict(frames[0])
			if err != nil {
				t.Fatal(err)
			}
			return message
		}
	}
}

func TestSessionLogonHeartbeatResendResetAndApplication(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	fixed := time.Date(2026, 9, 9, 16, 0, 0, 0, time.UTC)
	nonce := bytes.Repeat([]byte{1}, 64)
	auth := &Authenticator{ClientID: "id", ClientSecret: "secret", SenderCompID: "client", HeartbeatSeconds: 10, CancelOnDisconnect: false, ReportFillsAsExecReports: true, Now: func() time.Time { return fixed }, Random: bytes.NewReader(nonce)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var applications, recoveries atomic.Int32
	session, err := NewSession(SessionConfig{Dial: func(context.Context) (bybitfix.Transport, error) { return clientConn, nil }, Auth: auth, Heartbeat: time.Hour, Now: func() time.Time { return fixed }, OnMessage: func(context.Context, bybitfix.Message) error { applications.Add(1); cancel(); return nil }, OnRecovery: func(context.Context) error { recoveries.Add(1); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		logon := readMessage(t, serverConn)
		if value, _ := logon.Get(35); value != "A" {
			t.Errorf("logon type=%s", value)
		}
		_, _ = serverConn.Write(encodeServer(t, "A", 1))
		_, _ = serverConn.Write(encodeServer(t, "1", 2, bybitfix.Field{Tag: 112, Value: "probe"}))
		heartbeat := readMessage(t, serverConn)
		if typ, _ := heartbeat.Get(35); typ != "0" {
			t.Errorf("heartbeat type=%s", typ)
		}
		if id, _ := heartbeat.Get(112); id != "probe" {
			t.Errorf("test id=%s", id)
		}
		_, _ = serverConn.Write(encodeServer(t, "2", 3, bybitfix.Field{Tag: 7, Value: "2"}, bybitfix.Field{Tag: 16, Value: "2"}))
		replayed := readMessage(t, serverConn)
		if typ, _ := replayed.Get(35); typ != "0" {
			t.Errorf("replayed type=%s", typ)
		}
		if sequence, _ := replayed.Get(34); sequence != "2" {
			t.Errorf("replayed sequence=%s", sequence)
		}
		if duplicate, _ := replayed.Get(43); duplicate != "Y" {
			t.Errorf("replayed PossDupFlag=%s", duplicate)
		}
		if originalTime, _ := replayed.Get(122); originalTime == "" {
			t.Error("replayed OrigSendingTime missing")
		}
		_, _ = serverConn.Write(encodeServer(t, "4", 4, bybitfix.Field{Tag: 36, Value: "6"}))
		_, _ = serverConn.Write(encodeServer(t, "8", 6, bybitfix.Field{Tag: 37, Value: "order"}))
	}()
	if err := session.Run(ctx); err != nil {
		t.Fatal(err)
	}
	<-serverDone
	stats := session.Stats()
	if applications.Load() != 1 || recoveries.Load() != 1 || stats.TestRequests != 1 || stats.ResendRequests != 1 || stats.ResentMessages != 1 || stats.SequenceResets != 1 {
		t.Fatalf("apps=%d recovery=%d stats=%+v", applications.Load(), recoveries.Load(), stats)
	}
}

type memoryTransport struct{ bytes.Buffer }

func (*memoryTransport) Close() error { return nil }

func TestSequenceGapPausesWritesUntilForwardReset(t *testing.T) {
	transport := &memoryTransport{}
	fixed := time.Now()
	session := &Session{config: SessionConfig{Auth: &Authenticator{SenderCompID: "client"}, Now: func() time.Time { return fixed }}, transport: transport, nextOut: 2, expectedIn: 2, loggedOn: true}
	highRaw := encodeServer(t, "8", 3)
	high, _ := bybitfix.ParseStrict(highRaw)
	if err := session.handle(context.Background(), high); err != nil {
		t.Fatal(err)
	}
	if !session.Stats().Recovering {
		t.Fatal("gap did not enter recovery")
	}
	sent := readMessage(t, transport)
	if typ, _ := sent.Get(35); typ != "2" {
		t.Fatalf("sent type=%s", typ)
	}
	if err := session.Send("D", nil); err == nil {
		t.Fatal("write allowed during recovery")
	}
	resetRaw := encodeServer(t, "4", 2, bybitfix.Field{Tag: 36, Value: "4"})
	reset, _ := bybitfix.ParseStrict(resetRaw)
	if err := session.handle(context.Background(), reset); err != nil {
		t.Fatal(err)
	}
	if session.Stats().Recovering {
		t.Fatal("forward reset did not recover")
	}
}

func TestBackwardSequenceResetAndUnauthenticatedLogoutFailClosed(t *testing.T) {
	session := &Session{config: SessionConfig{Auth: &Authenticator{SenderCompID: "client"}, Now: time.Now}, transport: &memoryTransport{}, nextOut: 1, expectedIn: 2, loggedOn: true}
	resetRaw := encodeServer(t, "4", 999, bybitfix.Field{Tag: 36, Value: "2"})
	reset, _ := bybitfix.ParseStrict(resetRaw)
	if err := session.handle(context.Background(), reset); err == nil {
		t.Fatal("non-forward reset accepted")
	}
	session.expectedIn = 1
	session.loggedOn = false
	logoutRaw := encodeServer(t, "5", 1, bybitfix.Field{Tag: 58, Value: "secret echo must not be exposed"})
	logout, _ := bybitfix.ParseStrict(logoutRaw)
	err := session.handle(context.Background(), logout)
	if _, ok := err.(*AuthenticationError); !ok {
		t.Fatalf("err=%T %v", err, err)
	}
}

func TestSequenceResetIgnoresHeaderSequenceAndMovesToNewSequence(t *testing.T) {
	session := &Session{
		config:     SessionConfig{Auth: &Authenticator{SenderCompID: "client"}, Now: time.Now},
		transport:  &memoryTransport{},
		nextOut:    1,
		expectedIn: 2,
		loggedOn:   true,
	}
	resetRaw := encodeServer(t, "4", 999, bybitfix.Field{Tag: 36, Value: "4"})
	reset, _ := bybitfix.ParseStrict(resetRaw)
	if err := session.handle(context.Background(), reset); err != nil {
		t.Fatal(err)
	}
	if session.expectedIn != 4 || session.Stats().SequenceResets != 1 {
		t.Fatalf("expectedIn=%d stats=%+v", session.expectedIn, session.Stats())
	}
}
