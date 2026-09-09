package deribitfixmock

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"bybit/internal/deribitfix"
	bybitfix "bybit/internal/fix"
)

type AuthenticationError struct{}

func (*AuthenticationError) Error() string { return "mock Deribit FIX authentication rejected" }

type Server struct {
	ClientID       string
	ClientSecret   string
	Now            func() time.Time
	OrderLifecycle bool
}

func (s Server) Serve(ctx context.Context, transport bybitfix.Transport) error {
	if s.ClientID == "" || s.ClientSecret == "" {
		return errors.New("mock Deribit FIX credentials are required")
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	reader := messageReader{transport: transport, framer: bybitfix.Framer{MaxSize: 1 << 20}}

	logon, err := reader.read(ctx)
	if err != nil {
		return err
	}
	clientSender, err := s.validateLogon(logon)
	if err != nil {
		return err
	}
	if err := s.write(transport, clientSender, "A", 1, nil); err != nil {
		return err
	}
	if err := s.write(transport, clientSender, "1", 2, []bybitfix.Field{{Tag: 112, Value: "mock-probe"}}); err != nil {
		return err
	}
	heartbeat, err := reader.read(ctx)
	if err != nil {
		return err
	}
	if typ, _ := heartbeat.Get(35); typ != "0" {
		return fmt.Errorf("mock expected Heartbeat, got MsgType=%q", typ)
	}
	if testID, _ := heartbeat.Get(112); testID != "mock-probe" {
		return errors.New("mock Heartbeat did not echo TestReqID")
	}

	if err := s.write(transport, clientSender, "2", 3, []bybitfix.Field{
		{Tag: 7, Value: "2"},
		{Tag: 16, Value: "2"},
	}); err != nil {
		return err
	}
	replayed, err := reader.read(ctx)
	if err != nil {
		return err
	}
	if typ, _ := replayed.Get(35); typ != "0" {
		return fmt.Errorf("mock expected replayed Heartbeat, got MsgType=%q", typ)
	}
	if sequence, _ := replayed.Get(34); sequence != "2" {
		return errors.New("mock replay did not preserve original MsgSeqNum")
	}
	if duplicate, _ := replayed.Get(43); duplicate != "Y" {
		return errors.New("mock replay missing PossDupFlag")
	}
	if originalTime, _ := replayed.Get(122); originalTime == "" {
		return errors.New("mock replay missing OrigSendingTime")
	}

	if err := s.write(transport, clientSender, "4", 4, []bybitfix.Field{
		{Tag: 123, Value: "Y"},
		{Tag: 36, Value: "6"},
	}); err != nil {
		return err
	}
	if err := s.write(transport, clientSender, "0", 6, nil); err != nil {
		return err
	}
	if s.OrderLifecycle {
		if err := s.serveOrderLifecycle(ctx, transport, &reader, clientSender); err != nil {
			return err
		}
	}

	logout, err := reader.read(ctx)
	if err != nil {
		if errors.Is(err, io.EOF) && ctx.Err() != nil {
			return nil
		}
		return err
	}
	if typ, _ := logout.Get(35); typ != "5" {
		return fmt.Errorf("mock expected Logout, got MsgType=%q", typ)
	}
	return nil
}

func (s Server) serveOrderLifecycle(ctx context.Context, transport bybitfix.Transport, reader *messageReader, clientSender string) error {
	if err := s.write(transport, clientSender, "y", 7, []bybitfix.Field{
		{Tag: 320, Value: "mock-security"},
		{Tag: 146, Value: "1"},
		{Tag: 55, Value: "BTC-PERPETUAL"},
		{Tag: 167, Value: "FUT"},
		{Tag: 120, Value: "BTC"},
		{Tag: 231, Value: "10"},
		{Tag: 562, Value: "1"},
		{Tag: 454, Value: "0"},
		{Tag: 1205, Value: "0"},
	}); err != nil {
		return err
	}
	newOrder, err := reader.read(ctx)
	if err != nil {
		return err
	}
	if typ, _ := newOrder.Get(35); typ != "D" {
		return fmt.Errorf("mock expected NewOrderSingle, got MsgType=%q", typ)
	}
	clientOrderID, _ := newOrder.Get(11)
	label, _ := newOrder.Get(100010)
	amount, _ := newOrder.Get(38)
	price, _ := newOrder.Get(44)
	side, _ := newOrder.Get(54)
	symbol, _ := newOrder.Get(55)
	qtyType, _ := newOrder.Get(854)
	if clientOrderID == "" || label == "" || amount != "10" || symbol != "BTC-PERPETUAL" || qtyType != deribitfix.QtyTypeUnits {
		return errors.New("mock NewOrderSingle failed validated minimum-unit checks")
	}
	if err := s.writeExecutionReport(transport, clientSender, 8, clientOrderID, label, side, symbol, price, "0"); err != nil {
		return err
	}

	replace, err := reader.read(ctx)
	if err != nil {
		return err
	}
	if typ, _ := replace.Get(35); typ != "G" {
		return fmt.Errorf("mock expected OrderCancelReplaceRequest, got MsgType=%q", typ)
	}
	if original, _ := replace.Get(41); original != "mock-server-clord-1" {
		return errors.New("mock replace did not use server-assigned ClOrdID")
	}
	replacePrice, _ := replace.Get(44)
	if replacePrice == "" || replacePrice == price {
		return errors.New("mock replace did not change price")
	}
	if err := s.writeExecutionReport(transport, clientSender, 9, clientOrderID, label, side, symbol, replacePrice, "0"); err != nil {
		return err
	}

	cancel, err := reader.read(ctx)
	if err != nil {
		return err
	}
	if typ, _ := cancel.Get(35); typ != "F" {
		return fmt.Errorf("mock expected OrderCancelRequest, got MsgType=%q", typ)
	}
	if original, _ := cancel.Get(41); original != "mock-server-clord-1" {
		return errors.New("mock cancel did not use server-assigned ClOrdID")
	}
	return s.writeExecutionReport(transport, clientSender, 10, clientOrderID, label, side, symbol, replacePrice, "4")
}

func (s Server) writeExecutionReport(transport io.Writer, target string, sequence int64, clientOrderID, label, side, symbol, price, status string) error {
	leaves := "1"
	if status == "4" {
		leaves = "0"
	}
	return s.write(transport, target, "8", sequence, []bybitfix.Field{
		{Tag: 37, Value: "mock-order-1"},
		{Tag: 11, Value: "mock-server-clord-1"},
		{Tag: 41, Value: clientOrderID},
		{Tag: 100010, Value: label},
		{Tag: 39, Value: status},
		{Tag: 54, Value: side},
		{Tag: 60, Value: s.Now().UTC().Format("20060102-15:04:05.000")},
		{Tag: 151, Value: leaves},
		{Tag: 14, Value: "0"},
		{Tag: 38, Value: "1"},
		{Tag: 40, Value: "2"},
		{Tag: 44, Value: price},
		{Tag: 150, Value: "I"},
		{Tag: 103, Value: "0"},
		{Tag: 55, Value: symbol},
		{Tag: 854, Value: deribitfix.QtyTypeContracts},
		{Tag: 231, Value: "10"},
	})
}

func (s Server) validateLogon(message bybitfix.Message) (string, error) {
	required := map[int]string{
		35:   "A",
		56:   deribitfix.TargetCompID,
		34:   "1",
		98:   "0",
		553:  s.ClientID,
		9001: "N",
		9015: "Y",
	}
	for tag, want := range required {
		if got, ok := message.Get(tag); !ok || got != want {
			return "", &AuthenticationError{}
		}
	}
	heartbeat, ok := message.Get(108)
	if !ok {
		return "", &AuthenticationError{}
	}
	seconds, err := strconv.Atoi(heartbeat)
	if err != nil || seconds <= 0 {
		return "", &AuthenticationError{}
	}
	sender, ok := message.Get(49)
	if !ok || strings.TrimSpace(sender) == "" {
		return "", &AuthenticationError{}
	}
	rawData, ok := message.Get(96)
	if !ok {
		return "", &AuthenticationError{}
	}
	rawLength, ok := message.Get(95)
	if !ok || rawLength != strconv.Itoa(len(rawData)) {
		return "", &AuthenticationError{}
	}
	parts := strings.Split(rawData, ".")
	if len(parts) != 2 {
		return "", &AuthenticationError{}
	}
	if timestamp, err := strconv.ParseInt(parts[0], 10, 64); err != nil || timestamp <= 0 {
		return "", &AuthenticationError{}
	}
	nonce, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || len(nonce) != 32 {
		return "", &AuthenticationError{}
	}
	digest := sha256.Sum256([]byte(rawData + s.ClientSecret))
	wantPassword := base64.StdEncoding.EncodeToString(digest[:])
	password, ok := message.Get(554)
	if !ok || subtle.ConstantTimeCompare([]byte(password), []byte(wantPassword)) != 1 {
		return "", &AuthenticationError{}
	}
	return sender, nil
}

func (s Server) write(transport io.Writer, target, msgType string, sequence int64, body []bybitfix.Field) error {
	fields := []bybitfix.Field{
		{Tag: 35, Value: msgType},
		{Tag: 49, Value: deribitfix.TargetCompID},
		{Tag: 56, Value: target},
		{Tag: 34, Value: strconv.FormatInt(sequence, 10)},
		{Tag: 52, Value: s.Now().UTC().Format("20060102-15:04:05.000")},
	}
	fields = append(fields, body...)
	raw, err := bybitfix.Encode("FIX.4.4", fields)
	if err != nil {
		return err
	}
	_, err = transport.Write(raw)
	return err
}

type messageReader struct {
	transport io.Reader
	framer    bybitfix.Framer
	pending   [][]byte
}

func (r *messageReader) read(ctx context.Context) (bybitfix.Message, error) {
	for len(r.pending) == 0 {
		if err := ctx.Err(); err != nil {
			return bybitfix.Message{}, err
		}
		buffer := make([]byte, 4096)
		n, err := r.transport.Read(buffer)
		if n > 0 {
			frames, frameErr := r.framer.Feed(buffer[:n])
			if frameErr != nil {
				return bybitfix.Message{}, frameErr
			}
			r.pending = append(r.pending, frames...)
		}
		if err != nil {
			return bybitfix.Message{}, err
		}
	}
	frame := r.pending[0]
	r.pending = r.pending[1:]
	return bybitfix.ParseStrict(frame)
}
