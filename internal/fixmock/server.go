package fixmock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"bybit/internal/fix"
)

type Scenario string

const (
	Accepted           Scenario = "accepted"
	Rejected           Scenario = "rejected"
	PartialThenFilled  Scenario = "partial_then_filled"
	CancelAccepted     Scenario = "cancel_accepted"
	CancelRaceFilled   Scenario = "cancel_race_filled"
	DisconnectAfterNew Scenario = "disconnect_after_new"
)

type Server struct {
	Scenario Scenario
	now      func() time.Time
	seq      int64
	order    fix.Message
	framer   fix.Framer
	pending  []fix.Message
}

func New(scenario Scenario) *Server { return &Server{Scenario: scenario, now: time.Now, seq: 1} }

func (s *Server) Serve(ctx context.Context, transport fix.Transport) error {
	defer transport.Close()
	logon, err := s.read(ctx, transport)
	if err != nil {
		return err
	}
	if typeOf(logon) != "A" {
		return errors.New("mock expected Logon")
	}
	if value(logon, 34) != "1" {
		return errors.New("mock expected new session sequence 1")
	}
	if _, ok := logon.Get(553); !ok {
		return errors.New("mock Logon missing API key")
	}
	if err := s.send(transport, "A", nil); err != nil {
		return err
	}
	for {
		message, err := s.read(ctx, transport)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		switch typeOf(message) {
		case "0":
		case "1":
			id, _ := message.Get(112)
			if err := s.send(transport, "0", []fix.Field{{Tag: 112, Value: id}}); err != nil {
				return err
			}
		case "D":
			s.order = message
			if value(message, 30010) != "spot" {
				return s.reject(transport, message, "category must be spot")
			}
			if s.Scenario == Rejected {
				if err := s.reject(transport, message, "fixture rejection"); err != nil {
					return err
				}
				continue
			}
			if err := s.execution(transport, message, "0", "0", "0", nil); err != nil {
				return err
			}
			if s.Scenario == DisconnectAfterNew {
				return io.EOF
			}
			if s.Scenario == PartialThenFilled {
				if err := s.execution(transport, message, "F", "1", "0.4", []fix.Field{{Tag: 30050, Value: "1"}, {Tag: 30051, Value: "exec-1"}, {Tag: 30052, Value: "0.4"}, {Tag: 30053, Value: value(message, 44)}, {Tag: 30054, Value: s.timestamp()}}); err != nil {
					return err
				}
				if err := s.execution(transport, message, "F", "2", value(message, 38), []fix.Field{{Tag: 30050, Value: "1"}, {Tag: 30051, Value: "exec-2"}, {Tag: 30052, Value: "0.6"}, {Tag: 30053, Value: value(message, 44)}, {Tag: 30054, Value: s.timestamp()}}); err != nil {
					return err
				}
			}
		case "F":
			if s.Scenario == CancelRaceFilled {
				if err := s.execution(transport, s.order, "F", "2", value(s.order, 38), []fix.Field{{Tag: 30050, Value: "1"}, {Tag: 30051, Value: "race-fill"}, {Tag: 30052, Value: value(s.order, 38)}, {Tag: 30053, Value: value(s.order, 44)}, {Tag: 30054, Value: s.timestamp()}}); err != nil {
					return err
				}
				continue
			}
			if err := s.send(transport, "XCA", []fix.Field{{Tag: 37, Value: "mock-order-1"}, {Tag: 11, Value: value(message, 11)}, {Tag: 150, Value: "6"}, {Tag: 39, Value: "6"}}); err != nil {
				return err
			}
			if err := s.execution(transport, s.order, "4", "4", "0", nil); err != nil {
				return err
			}
		case "XAR":
			if err := s.send(transport, "XAA", []fix.Field{{Tag: 37, Value: "mock-order-1"}, {Tag: 11, Value: value(message, 11)}, {Tag: 150, Value: "E"}, {Tag: 39, Value: "E"}}); err != nil {
				return err
			}
			fields := []fix.Field{{Tag: 37, Value: "mock-order-1"}, {Tag: 11, Value: value(message, 11)}, {Tag: 150, Value: "5"}, {Tag: 39, Value: "0"}, {Tag: 30010, Value: "spot"}, {Tag: 55, Value: value(message, 55)}, {Tag: 38, Value: value(message, 38)}, {Tag: 44, Value: value(message, 44)}, {Tag: 14, Value: "0"}, {Tag: 60, Value: s.timestamp()}}
			if err := s.send(transport, "8", fields); err != nil {
				return err
			}
		case "5":
			_ = s.send(transport, "5", nil)
			return nil
		default:
			return fmt.Errorf("mock unsupported MsgType %q", typeOf(message))
		}
	}
}

func (s *Server) reject(w io.Writer, m fix.Message, text string) error {
	return s.send(w, "8", []fix.Field{{Tag: 11, Value: value(m, 11)}, {Tag: 150, Value: "8"}, {Tag: 39, Value: "8"}, {Tag: 103, Value: "99"}, {Tag: 58, Value: text}, {Tag: 60, Value: s.timestamp()}})
}
func (s *Server) execution(w io.Writer, m fix.Message, execType, status, cum string, extra []fix.Field) error {
	fields := []fix.Field{{Tag: 37, Value: "mock-order-1"}, {Tag: 11, Value: value(m, 11)}, {Tag: 150, Value: execType}, {Tag: 39, Value: status}, {Tag: 30010, Value: "spot"}, {Tag: 55, Value: value(m, 55)}, {Tag: 54, Value: value(m, 54)}, {Tag: 40, Value: value(m, 40)}, {Tag: 38, Value: value(m, 38)}, {Tag: 44, Value: value(m, 44)}, {Tag: 14, Value: cum}, {Tag: 6, Value: value(m, 44)}, {Tag: 60, Value: s.timestamp()}}
	fields = append(fields, extra...)
	return s.send(w, "8", fields)
}
func (s *Server) send(w io.Writer, msgType string, body []fix.Field) error {
	fields := []fix.Field{{Tag: 35, Value: msgType}, {Tag: 49, Value: "BYBIT_FIX_SERVER"}, {Tag: 56, Value: "FIX_CLIENT"}, {Tag: 34, Value: strconv.FormatInt(s.seq, 10)}, {Tag: 52, Value: s.timestamp()}}
	fields = append(fields, body...)
	encoded, err := fix.Encode("FIX.4.4", fields)
	if err != nil {
		return err
	}
	s.seq++
	for len(encoded) > 0 {
		n, err := w.Write(encoded)
		if err != nil {
			return err
		}
		encoded = encoded[n:]
	}
	return nil
}
func (s *Server) timestamp() string       { return s.now().UTC().Format("20060102-15:04:05.000") }
func value(m fix.Message, tag int) string { v, _ := m.Get(tag); return v }
func typeOf(m fix.Message) string         { return value(m, 35) }
func (s *Server) read(ctx context.Context, r io.Reader) (fix.Message, error) {
	if len(s.pending) > 0 {
		message := s.pending[0]
		s.pending = s.pending[1:]
		return message, nil
	}
	buffer := make([]byte, 1024)
	for {
		n, err := r.Read(buffer)
		if n > 0 {
			frames, frameErr := s.framer.Feed(buffer[:n])
			if frameErr != nil {
				return fix.Message{}, frameErr
			}
			if len(frames) > 0 {
				messages := make([]fix.Message, 0, len(frames))
				for _, frame := range frames {
					message, parseErr := fix.ParseStrict(frame)
					if parseErr != nil {
						return fix.Message{}, parseErr
					}
					messages = append(messages, message)
				}
				s.pending = append(s.pending, messages[1:]...)
				return messages[0], nil
			}
		}
		if err != nil {
			return fix.Message{}, err
		}
		select {
		case <-ctx.Done():
			return fix.Message{}, ctx.Err()
		default:
		}
	}
}

func PairDialer(ctx context.Context, scenario Scenario) (fix.DialFunc, <-chan error) {
	done := make(chan error, 1)
	var used bool
	return func(context.Context) (fix.Transport, error) {
		if used {
			return nil, errors.New("mock dialer supports one connection")
		}
		used = true
		client, server := net.Pipe()
		go func() { done <- New(scenario).Serve(ctx, server) }()
		return client, nil
	}, done
}
