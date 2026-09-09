package deribitfix

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	bybitfix "bybit/internal/fix"
)

func TestRecoveryBuffersAndDeliversOutOfOrderMessageExactlyOnce(t *testing.T) {
	transport := &memoryTransport{}
	var applications, recoveries atomic.Int32
	session := &Session{
		config: SessionConfig{
			Auth:              &Authenticator{SenderCompID: "client"},
			Now:               time.Now,
			MaxRecoveryBuffer: 2,
			OnMessage: func(context.Context, bybitfix.Message) error {
				applications.Add(1)
				return nil
			},
			OnRecovery: func(context.Context) error {
				recoveries.Add(1)
				return nil
			},
		},
		transport:  transport,
		nextOut:    2,
		expectedIn: 2,
		loggedOn:   true,
		pending:    make(map[int64]bybitfix.Message),
	}

	highRaw := encodeServer(t, "8", 3, bybitfix.Field{Tag: 37, Value: "order-1"})
	high, err := bybitfix.ParseStrict(highRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.handle(context.Background(), high); err != nil {
		t.Fatal(err)
	}
	if got := applications.Load(); got != 0 {
		t.Fatalf("out-of-order application delivered early: %d", got)
	}
	stats := session.Stats()
	if !stats.Recovering || stats.RecoveryBuffered != 1 {
		t.Fatalf("unexpected recovery state: %+v", stats)
	}
	request := readMessage(t, transport)
	if typ, _ := request.Get(35); typ != "2" {
		t.Fatalf("sent type=%q, want ResendRequest", typ)
	}

	missingRaw := encodeServer(t, "0", 2)
	missing, err := bybitfix.ParseStrict(missingRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.handle(context.Background(), missing); err != nil {
		t.Fatal(err)
	}
	if got := applications.Load(); got != 1 {
		t.Fatalf("buffered application deliveries=%d, want 1", got)
	}
	if got := recoveries.Load(); got != 1 {
		t.Fatalf("recovery callbacks=%d, want 1", got)
	}
	stats = session.Stats()
	if stats.Recovering || stats.RecoveryBuffered != 0 {
		t.Fatalf("recovery did not clear: %+v", stats)
	}
}

func TestRecoveryBufferOverflowFailsClosed(t *testing.T) {
	session := &Session{
		config: SessionConfig{
			Auth:              &Authenticator{SenderCompID: "client"},
			Now:               time.Now,
			MaxRecoveryBuffer: 1,
		},
		transport:  &memoryTransport{},
		nextOut:    1,
		expectedIn: 1,
		loggedOn:   true,
		pending:    make(map[int64]bybitfix.Message),
	}

	firstRaw := encodeServer(t, "8", 3)
	first, _ := bybitfix.ParseStrict(firstRaw)
	if err := session.handle(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	secondRaw := encodeServer(t, "8", 4)
	second, _ := bybitfix.ParseStrict(secondRaw)
	err := session.handle(context.Background(), second)
	var overflow *RecoveryOverflowError
	if !errors.As(err, &overflow) || overflow.Limit != 1 {
		t.Fatalf("error=%T %v, want bounded recovery overflow", err, err)
	}
	if !session.Stats().Recovering {
		t.Fatal("overflow incorrectly cleared recovery state")
	}
}

func TestResendRequestReplaysApplicationWithOriginalSequence(t *testing.T) {
	transport := &memoryTransport{}
	session := &Session{
		config: SessionConfig{
			Auth:               &Authenticator{SenderCompID: "client"},
			Now:                time.Now,
			MaxOutboundJournal: 2,
		},
		transport:  transport,
		nextOut:    1,
		expectedIn: 1,
		loggedOn:   true,
		journal:    make(map[int64][]bybitfix.Field),
	}
	if err := session.sendLocked("D", []bybitfix.Field{{Tag: 11, Value: "intent-1"}}); err != nil {
		t.Fatal(err)
	}
	original := readMessage(t, transport)
	requestRaw := encodeServer(t, "2", 1,
		bybitfix.Field{Tag: 7, Value: "1"},
		bybitfix.Field{Tag: 16, Value: "1"},
	)
	request, _ := bybitfix.ParseStrict(requestRaw)
	if err := session.handle(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	replayed := readMessage(t, transport)
	if sequence, _ := original.Get(34); sequence != "1" {
		t.Fatalf("original sequence=%q", sequence)
	}
	if sequence, _ := replayed.Get(34); sequence != "1" {
		t.Fatalf("replayed sequence=%q", sequence)
	}
	if duplicate, _ := replayed.Get(43); duplicate != "Y" {
		t.Fatalf("PossDupFlag=%q", duplicate)
	}
	if originalTime, _ := replayed.Get(122); originalTime == "" {
		t.Fatal("OrigSendingTime missing")
	}
	if clientID, _ := replayed.Get(11); clientID != "intent-1" {
		t.Fatalf("application body changed: ClOrdID=%q", clientID)
	}
	if session.nextOut != 2 || session.Stats().ResentMessages != 1 {
		t.Fatalf("replay advanced live sequence: next=%d stats=%+v", session.nextOut, session.Stats())
	}
}

func TestResendOutsideBoundedJournalFailsClosed(t *testing.T) {
	transport := &memoryTransport{}
	session := &Session{
		config: SessionConfig{
			Auth:               &Authenticator{SenderCompID: "client"},
			Now:                time.Now,
			MaxOutboundJournal: 1,
		},
		transport:  transport,
		nextOut:    1,
		expectedIn: 1,
		loggedOn:   true,
		journal:    make(map[int64][]bybitfix.Field),
	}
	if err := session.sendLocked("D", []bybitfix.Field{{Tag: 11, Value: "intent-1"}}); err != nil {
		t.Fatal(err)
	}
	if err := session.sendLocked("D", []bybitfix.Field{{Tag: 11, Value: "intent-2"}}); err != nil {
		t.Fatal(err)
	}
	requestRaw := encodeServer(t, "2", 1,
		bybitfix.Field{Tag: 7, Value: "1"},
		bybitfix.Field{Tag: 16, Value: "1"},
	)
	request, _ := bybitfix.ParseStrict(requestRaw)
	err := session.handle(context.Background(), request)
	var unavailable *ResendUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Sequence != 1 {
		t.Fatalf("error=%T %v", err, err)
	}
	if !session.Stats().Recovering {
		t.Fatal("missing resend evidence did not pause application writes")
	}
}

type partialWriteTransport struct {
	memoryTransport
	maxWrite int
}

func (p *partialWriteTransport) Write(data []byte) (int, error) {
	if len(data) > p.maxWrite {
		data = data[:p.maxWrite]
	}
	return p.memoryTransport.Write(data)
}

func TestSessionCompletesPartialTransportWrites(t *testing.T) {
	transport := &partialWriteTransport{maxWrite: 3}
	session := &Session{config: SessionConfig{Now: time.Now}, transport: transport}
	raw, err := bybitfix.Encode("FIX.4.4", []bybitfix.Field{{Tag: 35, Value: "0"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.writeRawLocked(raw); err != nil {
		t.Fatal(err)
	}
	if got := transport.Bytes(); string(got) != string(raw) {
		t.Fatalf("partial write lost bytes: got=%q want=%q", got, raw)
	}
}

func TestRunCancellationSendsLogoutAndJoinsReader(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	auth := &Authenticator{
		ClientID:                 "id",
		ClientSecret:             "secret",
		SenderCompID:             "client",
		HeartbeatSeconds:         10,
		ReportFillsAsExecReports: true,
	}
	session, err := NewSession(SessionConfig{
		Dial:      func(context.Context) (bybitfix.Transport, error) { return clientConn, nil },
		Auth:      auth,
		Heartbeat: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		logon := readMessage(t, serverConn)
		if typ, _ := logon.Get(35); typ != "A" {
			t.Errorf("first message type=%q, want Logon", typ)
			return
		}
		_, _ = serverConn.Write(encodeServer(t, "A", 1))
		logout := readMessage(t, serverConn)
		if typ, _ := logout.Get(35); typ != "5" {
			t.Errorf("last message type=%q, want Logout", typ)
		}
	}()
	go func() { runDone <- session.Run(ctx) }()

	deadline := time.After(2 * time.Second)
	for !session.Stats().LoggedOn {
		select {
		case <-deadline:
			t.Fatal("session did not log on")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned error after cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not join its reader after cancellation")
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("mock server did not observe Logout")
	}
	if session.Stats().LoggedOn {
		t.Fatal("session remained logged on after Run returned")
	}
}
