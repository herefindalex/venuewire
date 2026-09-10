package deribitfix

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	bybitfix "venuewire/internal/fix"
)

const TargetCompID = "DERIBITSERVER"

type Authenticator struct {
	ClientID                 string
	ClientSecret             string
	SenderCompID             string
	HeartbeatSeconds         int
	CancelOnDisconnect       bool
	ReportFillsAsExecReports bool
	Now                      func() time.Time
	Random                   io.Reader
	mu                       sync.Mutex
	lastTimestamp            int64
}

func (a *Authenticator) Logon(seq int64) ([]byte, error) {
	if a.ClientID == "" || a.ClientSecret == "" || a.SenderCompID == "" {
		return nil, errors.New("Deribit FIX client ID, secret, and SenderCompID are required")
	}
	if a.HeartbeatSeconds <= 0 {
		return nil, errors.New("Deribit FIX heartbeat must be positive")
	}
	if a.Now == nil {
		a.Now = time.Now
	}
	if a.Random == nil {
		a.Random = rand.Reader
	}
	nonce := make([]byte, 32)
	if _, err := io.ReadFull(a.Random, nonce); err != nil {
		return nil, fmt.Errorf("generate Deribit FIX nonce: %w", err)
	}
	a.mu.Lock()
	timestamp := a.Now().UTC().UnixMilli()
	if timestamp <= a.lastTimestamp {
		timestamp = a.lastTimestamp + 1
	}
	a.lastTimestamp = timestamp
	a.mu.Unlock()
	rawData := strconv.FormatInt(timestamp, 10) + "." + base64.StdEncoding.EncodeToString(nonce)
	digest := sha256.Sum256(append([]byte(rawData), []byte(a.ClientSecret)...))
	password := base64.StdEncoding.EncodeToString(digest[:])
	cod := "N"
	if a.CancelOnDisconnect {
		cod = "Y"
	}
	fills := "N"
	if a.ReportFillsAsExecReports {
		fills = "Y"
	}
	now := a.Now().UTC()
	fields := []bybitfix.Field{{Tag: 35, Value: "A"}, {Tag: 49, Value: a.SenderCompID}, {Tag: 56, Value: TargetCompID}, {Tag: 34, Value: strconv.FormatInt(seq, 10)}, {Tag: 52, Value: now.Format("20060102-15:04:05.000")}, {Tag: 98, Value: "0"}, {Tag: 108, Value: strconv.Itoa(a.HeartbeatSeconds)}, {Tag: 95, Value: strconv.Itoa(len(rawData))}, {Tag: 96, Value: rawData}, {Tag: 553, Value: a.ClientID}, {Tag: 554, Value: password}, {Tag: 9001, Value: cod}, {Tag: 9015, Value: fills}}
	return bybitfix.Encode("FIX.4.4", fields)
}
