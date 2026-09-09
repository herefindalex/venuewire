package fix

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type AccessDeniedError struct{ Text string }

func (e *AccessDeniedError) Error() string { return "FIX access denied: " + e.Text }

type SequenceError struct{ Expected, Received int64 }

func (e *SequenceError) Error() string {
	return fmt.Sprintf("unexpected FIX MsgSeqNum: expected=%d received=%d; Bybit sessions do not support gap-fill recovery", e.Expected, e.Received)
}

type SessionConfig struct {
	Dial           DialFunc
	SenderCompID   string
	TargetCompID   string
	APIKey         string
	Signer         AuthSigner
	Heartbeat      time.Duration
	Now            func() time.Time
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	OnMessage      func(context.Context, Message) error
	OnReconnect    func(context.Context) error
}

type SessionStats struct {
	Inbound, Outbound, Reconnects, SequenceErrors, Heartbeats, TestRequests uint64
}

type Session struct {
	config         SessionConfig
	writeMu        sync.Mutex
	connMu         sync.RWMutex
	active         Transport
	outboundSeq    int64
	inboundSeq     int64
	inbound        atomic.Uint64
	outbound       atomic.Uint64
	reconnects     atomic.Uint64
	sequenceErrors atomic.Uint64
	heartbeats     atomic.Uint64
	testRequests   atomic.Uint64
	lastOutboundNS atomic.Int64
}

func NewSession(config SessionConfig) (*Session, error) {
	if config.Dial == nil {
		return nil, errors.New("FIX dialer is required")
	}
	if config.TargetCompID == "" {
		config.TargetCompID = "BYBIT_FIX_SERVER"
	}
	if config.Heartbeat <= 0 {
		config.Heartbeat = 10 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.InitialBackoff <= 0 {
		config.InitialBackoff = 250 * time.Millisecond
	}
	if config.MaxBackoff <= 0 {
		config.MaxBackoff = 15 * time.Second
	}
	return &Session{config: config}, nil
}

func (s *Session) Run(ctx context.Context) error {
	backoff := s.config.InitialBackoff
	attempt := 0
	for {
		currentAttempt := attempt
		err := s.runOnce(ctx, func(logonContext context.Context) error {
			if currentAttempt == 0 {
				return nil
			}
			s.reconnects.Add(1)
			if s.config.OnReconnect != nil {
				return s.config.OnReconnect(logonContext)
			}
			return nil
		})
		if ctx.Err() != nil {
			return nil
		}
		var denied *AccessDeniedError
		if errors.As(err, &denied) {
			return err
		}
		attempt++
		if err == nil {
			return nil
		}
		timer := time.NewTimer(sessionJitter(backoff))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff *= 2
		if backoff > s.config.MaxBackoff {
			backoff = s.config.MaxBackoff
		}
	}
}

func (s *Session) RunOnce(ctx context.Context) error {
	return s.runOnce(ctx, nil)
}

func (s *Session) runOnce(ctx context.Context, onLogon func(context.Context) error) error {
	transport, err := s.config.Dial(ctx)
	if err != nil {
		return err
	}
	defer transport.Close()
	s.writeMu.Lock()
	s.outboundSeq = 1
	s.writeMu.Unlock()
	s.inboundSeq = 1
	if err := s.sendLogon(transport); err != nil {
		return err
	}
	s.connMu.Lock()
	s.active = transport
	s.connMu.Unlock()
	defer func() {
		s.connMu.Lock()
		if s.active == transport {
			s.active = nil
		}
		s.connMu.Unlock()
	}()
	messages := make(chan Message, 16)
	readErrors := make(chan error, 1)
	readContext, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	go readMessages(readContext, transport, messages, readErrors)
	lastInbound := s.config.Now()
	loggedOn := false
	testRequestOutstanding := false
	tickerInterval := s.config.Heartbeat / 4
	if tickerInterval < 10*time.Millisecond {
		tickerInterval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if deadlineTransport, ok := transport.(interface{ SetWriteDeadline(time.Time) error }); ok {
				_ = deadlineTransport.SetWriteDeadline(time.Now().Add(2 * time.Second))
			}
			_ = s.send(transport, "5", nil)
			return nil
		case err := <-readErrors:
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return io.EOF
			}
			return err
		case message := <-messages:
			lastInbound = s.config.Now()
			s.inbound.Add(1)
			seq, err := messageSequence(message)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			if seq != s.inboundSeq {
				s.sequenceErrors.Add(1)
				return &SequenceError{Expected: s.inboundSeq, Received: seq}
			}
			s.inboundSeq++
			msgType, _ := message.Get(35)
			if !loggedOn && msgType != "A" && msgType != "5" {
				return fmt.Errorf("expected Logon ACK, received MsgType %q", msgType)
			}
			switch msgType {
			case "A":
				if !loggedOn && onLogon != nil {
					if err := onLogon(ctx); err != nil {
						if ctx.Err() != nil {
							return nil
						}
						return err
					}
				}
				loggedOn = true
			case "0":
				testRequestOutstanding = false
			case "1":
				s.testRequests.Add(1)
				requestID, _ := message.Get(112)
				if err := s.send(transport, "0", []Field{{112, requestID}}); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
			case "5":
				text, _ := message.Get(58)
				if strings.Contains(strings.ToLower(text), "access denied") {
					return &AccessDeniedError{Text: text}
				}
				_ = s.send(transport, "5", nil)
				return nil
			default:
				if s.config.OnMessage != nil {
					if err := s.config.OnMessage(ctx, message); err != nil {
						if ctx.Err() != nil {
							return nil
						}
						return err
					}
				}
			}
		case <-ticker.C:
			now := s.config.Now()
			lastOutbound := time.Unix(0, s.lastOutboundNS.Load())
			if loggedOn && now.Sub(lastOutbound) >= s.config.Heartbeat {
				if err := s.send(transport, "0", nil); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				s.heartbeats.Add(1)
			}
			if loggedOn && now.Sub(lastInbound) >= 2*s.config.Heartbeat && !testRequestOutstanding {
				id := "test-" + strconv.FormatInt(now.UnixMilli(), 10)
				if err := s.send(transport, "1", []Field{{112, id}}); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				testRequestOutstanding = true
				s.testRequests.Add(1)
			}
			if loggedOn && now.Sub(lastInbound) >= 3*s.config.Heartbeat {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("FIX session stale after unanswered TestRequest")
			}
		}
	}
}

func (s *Session) SendApplication(msgType string, fields []Field) error {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	transport := s.active
	if transport == nil {
		return errors.New("FIX session is not connected")
	}
	return s.send(transport, msgType, fields)
}

func (s *Session) sendLogon(transport Transport) error {
	if s.config.APIKey == "" || s.config.Signer == nil {
		return errors.New("FIX API key and RSA signer are required")
	}
	expires := s.config.Now().UTC().UnixMilli() + 5000
	signature, err := s.config.Signer.Sign([]byte("GET/realtime" + strconv.FormatInt(expires, 10)))
	if err != nil {
		return err
	}
	heartbeatSeconds := int64(s.config.Heartbeat / time.Second)
	if heartbeatSeconds < 1 {
		heartbeatSeconds = 1
	}
	return s.send(transport, "A", []Field{{98, "0"}, {108, strconv.FormatInt(heartbeatSeconds, 10)}, {553, s.config.APIKey}, {95, strconv.Itoa(len([]byte(signature)))}, {96, signature}, {141, "Y"}, {30023, strconv.FormatInt(expires, 10)}})
}

func (s *Session) send(transport Transport, msgType string, body []Field) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	fields := []Field{{35, msgType}, {49, s.config.SenderCompID}, {56, s.config.TargetCompID}, {34, strconv.FormatInt(s.outboundSeq, 10)}, {52, s.config.Now().UTC().Format("20060102-15:04:05.000")}}
	fields = append(fields, body...)
	encoded, err := Encode("FIX.4.4", fields)
	if err != nil {
		return err
	}
	if err := writeAll(transport, encoded); err != nil {
		return err
	}
	s.outboundSeq++
	s.outbound.Add(1)
	s.lastOutboundNS.Store(s.config.Now().UnixNano())
	return nil
}

func messageSequence(message Message) (int64, error) {
	value, ok := message.Get(34)
	if !ok {
		return 0, errors.New("FIX message missing MsgSeqNum(34)")
	}
	seq, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seq < 1 {
		return 0, fmt.Errorf("invalid MsgSeqNum(34) %q", value)
	}
	return seq, nil
}

func readMessages(ctx context.Context, reader io.Reader, messages chan<- Message, failures chan<- error) {
	framer := &Framer{}
	buffer := make([]byte, 4096)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			frames, frameErr := framer.Feed(buffer[:count])
			if frameErr != nil {
				reportSessionError(failures, frameErr)
				return
			}
			for _, frame := range frames {
				message, parseErr := ParseStrict(frame)
				if parseErr != nil {
					reportSessionError(failures, parseErr)
					return
				}
				select {
				case messages <- message:
				case <-ctx.Done():
					return
				}
			}
		}
		if err != nil {
			reportSessionError(failures, err)
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func reportSessionError(channel chan<- error, err error) {
	select {
	case channel <- err:
	default:
	}
}
func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		count, err := writer.Write(data)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		data = data[count:]
	}
	return nil
}

func (s *Session) Stats() SessionStats {
	return SessionStats{Inbound: s.inbound.Load(), Outbound: s.outbound.Load(), Reconnects: s.reconnects.Load(), SequenceErrors: s.sequenceErrors.Load(), Heartbeats: s.heartbeats.Load(), TestRequests: s.testRequests.Load()}
}

func sessionJitter(base time.Duration) time.Duration {
	if base <= 1 {
		return base
	}
	return base/2 + time.Duration(rand.Int64N(int64(base/2)))
}
