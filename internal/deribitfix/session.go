package deribitfix

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	bybitfix "github.com/herefindalex/venuewire/internal/fix"
)

const (
	defaultMaxRecoveryBuffer  = 64
	defaultMaxOutboundJournal = 256
)

type SequenceError struct{ Expected, Received int64 }

func (e *SequenceError) Error() string {
	return fmt.Sprintf("Deribit FIX sequence anomaly: expected=%d received=%d", e.Expected, e.Received)
}

type RecoveryOverflowError struct{ Limit int }

func (e *RecoveryOverflowError) Error() string {
	return fmt.Sprintf("Deribit FIX recovery buffer limit reached: %d", e.Limit)
}

type ResendUnavailableError struct{ Sequence int64 }

func (e *ResendUnavailableError) Error() string {
	return fmt.Sprintf("Deribit FIX resend unavailable for sequence %d", e.Sequence)
}

type AuthenticationError struct{}

func (*AuthenticationError) Error() string { return "Deribit FIX authentication rejected" }

type SessionConfig struct {
	Dial               bybitfix.DialFunc
	Auth               *Authenticator
	Heartbeat          time.Duration
	Now                func() time.Time
	MaxRecoveryBuffer  int
	MaxOutboundJournal int
	OnMessage          func(context.Context, bybitfix.Message) error
	// OnRecovery must reconcile business state over JSON-RPC. Application sends
	// remain paused until it returns successfully.
	OnRecovery func(context.Context) error
}

type SessionStats struct {
	Inbound, Outbound, Heartbeats, TestRequests, ResendRequests, ResentMessages, SequenceResets, SequenceErrors uint64
	RecoveryBuffered                                                                                            int
	LoggedOn, Recovering                                                                                        bool
}

type Session struct {
	config     SessionConfig
	mu         sync.Mutex
	transport  bybitfix.Transport
	nextOut    int64
	expectedIn int64
	loggedOn   bool
	recovering bool
	pending    map[int64]bybitfix.Message
	journal    map[int64][]bybitfix.Field
	lastWrite  time.Time

	inbound        atomic.Uint64
	outbound       atomic.Uint64
	heartbeats     atomic.Uint64
	testRequests   atomic.Uint64
	resendRequests atomic.Uint64
	sequenceResets atomic.Uint64
	sequenceErrors atomic.Uint64
	resentMessages atomic.Uint64
}

func NewSession(config SessionConfig) (*Session, error) {
	if config.Dial == nil || config.Auth == nil {
		return nil, errors.New("Deribit FIX dialer and authenticator are required")
	}
	if config.Heartbeat <= 0 {
		config.Heartbeat = 10 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MaxRecoveryBuffer <= 0 {
		config.MaxRecoveryBuffer = defaultMaxRecoveryBuffer
	}
	if config.MaxOutboundJournal <= 0 {
		config.MaxOutboundJournal = defaultMaxOutboundJournal
	}
	return &Session{
		config:     config,
		nextOut:    1,
		expectedIn: 1,
		pending:    make(map[int64]bybitfix.Message),
		journal:    make(map[int64][]bybitfix.Field),
	}, nil
}

func (s *Session) Run(ctx context.Context) error {
	transport, err := s.config.Dial(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.transport = transport
	logon, err := s.config.Auth.Logon(s.nextOut)
	if err == nil {
		err = s.writeRawLocked(logon)
		if err == nil {
			s.nextOut++
		}
	}
	s.mu.Unlock()
	if err != nil {
		_ = transport.Close()
		return err
	}

	reads := make(chan []byte, 8)
	readErrors := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buffer := make([]byte, 32<<10)
		for {
			n, readErr := transport.Read(buffer)
			if n > 0 {
				copyBytes := append([]byte(nil), buffer[:n]...)
				select {
				case reads <- copyBytes:
				case <-ctx.Done():
					return
				}
			}
			if readErr != nil {
				select {
				case readErrors <- readErr:
				case <-ctx.Done():
				}
				return
			}
		}
	}()
	defer func() {
		_ = transport.Close()
		<-readerDone
		s.mu.Lock()
		if s.transport == transport {
			s.transport = nil
			s.loggedOn = false
		}
		s.mu.Unlock()
	}()

	tickerInterval := s.config.Heartbeat / 2
	if tickerInterval < time.Millisecond {
		tickerInterval = time.Millisecond
	}
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()
	framer := bybitfix.Framer{MaxSize: 1 << 20}
	for {
		select {
		case <-ctx.Done():
			s.bestEffortLogout()
			return nil
		case readErr := <-readErrors:
			if errors.Is(readErr, io.EOF) && ctx.Err() != nil {
				return nil
			}
			return readErr
		case chunk := <-reads:
			frames, frameErr := framer.Feed(chunk)
			if frameErr != nil {
				return frameErr
			}
			for _, frame := range frames {
				message, parseErr := bybitfix.ParseStrict(frame)
				if parseErr != nil {
					return parseErr
				}
				if handleErr := s.handle(ctx, message); handleErr != nil {
					return handleErr
				}
			}
		case <-ticker.C:
			s.mu.Lock()
			if s.loggedOn && !s.recovering && s.config.Now().Sub(s.lastWrite) >= s.config.Heartbeat {
				err = s.sendLocked("0", nil)
				if err == nil {
					s.heartbeats.Add(1)
				}
			}
			s.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
}

func (s *Session) bestEffortLogout() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loggedOn || s.transport == nil {
		return
	}
	if deadlineTransport, ok := s.transport.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = deadlineTransport.SetWriteDeadline(time.Now().Add(2 * time.Second))
	}
	_ = s.sendLocked("5", nil)
	s.loggedOn = false
}

func (s *Session) Send(msgType string, fields []bybitfix.Field) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loggedOn {
		return errors.New("Deribit FIX session is not logged on")
	}
	if s.recovering {
		return errors.New("Deribit FIX session recovering; application writes paused")
	}
	return s.sendLocked(msgType, fields)
}

func (s *Session) handle(ctx context.Context, message bybitfix.Message) error {
	msgType, _ := message.Get(35)
	seqRaw, ok := message.Get(34)
	if !ok {
		return errors.New("Deribit FIX message missing MsgSeqNum")
	}
	seq, err := strconv.ParseInt(seqRaw, 10, 64)
	if err != nil || seq <= 0 {
		return errors.New("invalid Deribit FIX MsgSeqNum")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.inbound.Add(1)
	if msgType == "4" {
		return s.handleSequenceResetLocked(ctx, message)
	}

	if seq > s.expectedIn {
		if s.pending == nil {
			s.pending = make(map[int64]bybitfix.Message)
		}
		if s.config.MaxRecoveryBuffer <= 0 {
			s.config.MaxRecoveryBuffer = defaultMaxRecoveryBuffer
		}
		if _, exists := s.pending[seq]; !exists {
			if len(s.pending) >= s.config.MaxRecoveryBuffer {
				s.sequenceErrors.Add(1)
				return &RecoveryOverflowError{Limit: s.config.MaxRecoveryBuffer}
			}
			s.pending[seq] = message
		}
		if !s.recovering {
			s.recovering = true
			s.sequenceErrors.Add(1)
			if err := s.sendLocked("2", []bybitfix.Field{
				{Tag: 7, Value: strconv.FormatInt(s.expectedIn, 10)},
				{Tag: 16, Value: "0"},
			}); err != nil {
				return err
			}
			s.resendRequests.Add(1)
		}
		return nil
	}
	if seq < s.expectedIn {
		if duplicate, _ := message.Get(43); duplicate == "Y" {
			return nil
		}
		s.sequenceErrors.Add(1)
		return &SequenceError{Expected: s.expectedIn, Received: seq}
	}

	if err := s.processExpectedLocked(ctx, message); err != nil {
		return err
	}
	for {
		pending, exists := s.pending[s.expectedIn]
		if !exists {
			break
		}
		delete(s.pending, s.expectedIn)
		if err := s.processExpectedLocked(ctx, pending); err != nil {
			return err
		}
	}

	return s.finishRecoveryLocked(ctx)
}

func (s *Session) handleSequenceResetLocked(ctx context.Context, message bybitfix.Message) error {
	if !s.loggedOn {
		return errors.New("Deribit FIX SequenceReset arrived before Logon")
	}
	newRaw, ok := message.Get(36)
	if !ok {
		return errors.New("SequenceReset missing NewSeqNo")
	}
	next, err := strconv.ParseInt(newRaw, 10, 64)
	if err != nil || next <= s.expectedIn {
		return errors.New("SequenceReset may only move forward")
	}
	// Deribit's dialect explicitly ignores MsgSeqNum(34) on SequenceReset.
	s.expectedIn = next
	s.recovering = true
	for pendingSeq := range s.pending {
		if pendingSeq < next {
			delete(s.pending, pendingSeq)
		}
	}
	s.sequenceResets.Add(1)
	for {
		pending, exists := s.pending[s.expectedIn]
		if !exists {
			break
		}
		delete(s.pending, s.expectedIn)
		if err := s.processExpectedLocked(ctx, pending); err != nil {
			return err
		}
	}
	return s.finishRecoveryLocked(ctx)
}

func (s *Session) finishRecoveryLocked(ctx context.Context) error {
	if !s.recovering || len(s.pending) != 0 {
		return nil
	}
	if s.config.OnRecovery != nil {
		callback := s.config.OnRecovery
		s.mu.Unlock()
		err := callback(ctx)
		s.mu.Lock()
		if err != nil {
			return err
		}
	}
	s.recovering = false
	return nil
}

func (s *Session) processExpectedLocked(ctx context.Context, message bybitfix.Message) error {
	seqRaw, _ := message.Get(34)
	seq, _ := strconv.ParseInt(seqRaw, 10, 64)
	if seq != s.expectedIn {
		return &SequenceError{Expected: s.expectedIn, Received: seq}
	}
	s.expectedIn++

	msgType, _ := message.Get(35)
	switch msgType {
	case "A":
		if seq != 1 {
			return errors.New("Deribit FIX Logon must begin sequence 1")
		}
		s.loggedOn = true
	case "0":
		// Heartbeat.
	case "1":
		id, _ := message.Get(112)
		fields := []bybitfix.Field{}
		if id != "" {
			fields = append(fields, bybitfix.Field{Tag: 112, Value: id})
		}
		if err := s.sendLocked("0", fields); err != nil {
			return err
		}
		s.testRequests.Add(1)
	case "2":
		beginRaw, beginOK := message.Get(7)
		endRaw, endOK := message.Get(16)
		begin, beginErr := strconv.ParseInt(beginRaw, 10, 64)
		end, endErr := strconv.ParseInt(endRaw, 10, 64)
		if !beginOK || !endOK || beginErr != nil || endErr != nil || begin <= 0 || end < 0 {
			return errors.New("invalid Deribit FIX ResendRequest range")
		}
		if end == 0 || end >= s.nextOut {
			end = s.nextOut - 1
		}
		if end < begin {
			return errors.New("invalid Deribit FIX ResendRequest range")
		}
		if err := s.resendLocked(begin, end); err != nil {
			s.recovering = true
			return err
		}
		s.resendRequests.Add(1)
	case "4":
		return errors.New("internal Deribit FIX SequenceReset routing error")
	case "3":
		return errors.New("Deribit FIX session rejected message")
	case "5":
		if !s.loggedOn {
			return &AuthenticationError{}
		}
		s.loggedOn = false
		return io.EOF
	default:
		if !s.loggedOn {
			return errors.New("Deribit FIX application message arrived before Logon")
		}
		if s.config.OnMessage != nil {
			callback := s.config.OnMessage
			s.mu.Unlock()
			err := callback(ctx, message)
			s.mu.Lock()
			return err
		}
	}
	return nil
}

func (s *Session) sendLocked(msgType string, body []bybitfix.Field) error {
	fields := []bybitfix.Field{
		{Tag: 35, Value: msgType},
		{Tag: 49, Value: s.config.Auth.SenderCompID},
		{Tag: 56, Value: TargetCompID},
		{Tag: 34, Value: strconv.FormatInt(s.nextOut, 10)},
		{Tag: 52, Value: s.config.Now().UTC().Format("20060102-15:04:05.000")},
	}
	fields = append(fields, body...)
	raw, err := bybitfix.Encode("FIX.4.4", fields)
	if err != nil {
		return err
	}
	if err := s.writeRawLocked(raw); err != nil {
		return err
	}
	s.rememberOutboundLocked(s.nextOut, fields)
	s.nextOut++
	return nil
}

func (s *Session) rememberOutboundLocked(sequence int64, fields []bybitfix.Field) {
	if s.journal == nil {
		s.journal = make(map[int64][]bybitfix.Field)
	}
	if s.config.MaxOutboundJournal <= 0 {
		s.config.MaxOutboundJournal = defaultMaxOutboundJournal
	}
	if len(s.journal) >= s.config.MaxOutboundJournal {
		oldest := sequence
		for existing := range s.journal {
			if existing < oldest {
				oldest = existing
			}
		}
		delete(s.journal, oldest)
	}
	s.journal[sequence] = append([]bybitfix.Field(nil), fields...)
}

func (s *Session) resendLocked(begin, end int64) error {
	for sequence := begin; sequence <= end; sequence++ {
		original, ok := s.journal[sequence]
		if !ok {
			return &ResendUnavailableError{Sequence: sequence}
		}
		origSendingTime := ""
		for _, field := range original {
			if field.Tag == 52 {
				origSendingTime = field.Value
				break
			}
		}
		if origSendingTime == "" {
			return &ResendUnavailableError{Sequence: sequence}
		}
		resent := make([]bybitfix.Field, 0, len(original)+2)
		for _, field := range original {
			if field.Tag == 43 || field.Tag == 122 {
				continue
			}
			if field.Tag == 52 {
				resent = append(resent,
					bybitfix.Field{Tag: 52, Value: s.config.Now().UTC().Format("20060102-15:04:05.000")},
					bybitfix.Field{Tag: 43, Value: "Y"},
					bybitfix.Field{Tag: 122, Value: origSendingTime},
				)
				continue
			}
			resent = append(resent, field)
		}
		raw, err := bybitfix.Encode("FIX.4.4", resent)
		if err != nil {
			return err
		}
		if err := s.writeRawLocked(raw); err != nil {
			return err
		}
		s.resentMessages.Add(1)
	}
	return nil
}

func (s *Session) writeRawLocked(raw []byte) error {
	if s.transport == nil {
		return errors.New("Deribit FIX transport unavailable")
	}
	written := 0
	for written < len(raw) {
		n, err := s.transport.Write(raw[written:])
		written += n
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
	}
	s.lastWrite = s.config.Now()
	s.outbound.Add(1)
	return nil
}

func (s *Session) Stats() SessionStats {
	s.mu.Lock()
	logged, recovering := s.loggedOn, s.recovering
	buffered := len(s.pending)
	s.mu.Unlock()
	return SessionStats{
		Inbound:          s.inbound.Load(),
		Outbound:         s.outbound.Load(),
		Heartbeats:       s.heartbeats.Load(),
		TestRequests:     s.testRequests.Load(),
		ResendRequests:   s.resendRequests.Load(),
		ResentMessages:   s.resentMessages.Load(),
		SequenceResets:   s.sequenceResets.Load(),
		SequenceErrors:   s.sequenceErrors.Load(),
		RecoveryBuffered: buffered,
		LoggedOn:         logged,
		Recovering:       recovering,
	}
}
