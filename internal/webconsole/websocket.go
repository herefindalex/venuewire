package webconsole

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"venuewire/internal/accountstate"
	"venuewire/internal/domain"
	"venuewire/internal/runtimeevent"
)

var errUnsupportedBrowserMessage = errors.New("unsupported browser WebSocket message")

type browserEnvelope struct {
	SchemaVersion int          `json:"schemaVersion"`
	InstanceID    string       `json:"instanceId"`
	Sequence      uint64       `json:"seq"`
	Type          string       `json:"type"`
	Venue         domain.Venue `json:"venue"`
	AccountAlias  string       `json:"accountAlias"`
	StateRevision uint64       `json:"stateRevision"`
	SentAt        string       `json:"sentAt"`
	Payload       any          `json:"payload"`
}

type browserControl struct {
	Type  string       `json:"type"`
	Venue domain.Venue `json:"venue,omitempty"`
}

type browserSnapshotPayload struct {
	Accounts map[domain.Venue]accountstate.Snapshot `json:"accounts"`
	Trades   []publicTrade                          `json:"trades"`
	Health   []publicVenueStatus                    `json:"health"`
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.validWriteOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid_request_origin", "The request was rejected.", requestID(r))
		return
	}
	sess := sessionFrom(r)
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
		CheckOrigin: func(request *http.Request) bool {
			return request.Header.Get("Origin") == s.config.PublicOrigin
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	if !s.sessions.addConn(sess.ID, conn, 5) {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session connection limit"), time.Now().Add(time.Second))
		_ = conn.Close()
		return
	}
	defer func() {
		s.sessions.removeConn(sess.ID, conn)
		_ = conn.Close()
	}()

	events, cursor, unsubscribe := s.events.SubscribeFrom(64)
	defer unsubscribe()
	controls := make(chan browserControl, 4)
	readErrors := make(chan error, 1)
	conn.SetReadLimit(16 << 10)
	_ = conn.SetReadDeadline(sessionReadDeadline(s.now(), sess.ExpiresAt))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(sessionReadDeadline(s.now(), sess.ExpiresAt))
	})
	go readBrowserSocket(conn, sess.ExpiresAt, controls, readErrors)

	if err := s.writeBrowserEnvelope(conn, browserEnvelope{
		SchemaVersion: 1,
		InstanceID:    s.instanceID,
		Type:          "session.ready",
		StateRevision: cursor,
		SentAt:        s.now().UTC().Format(time.RFC3339Nano),
		Payload: map[string]any{
			"expiresAt": sess.ExpiresAt.UTC().Format(time.RFC3339),
			"readOnly":  !s.config.TradingEnabled,
		},
	}); err != nil {
		return
	}

	sequence := uint64(0)
	if err := s.writeBrowserSnapshot(r.Context(), conn, &sequence, cursor); err != nil {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "snapshot unavailable"), time.Now().Add(time.Second))
		return
	}

	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()
	expiryTimer := time.NewTimer(maxDuration(0, sess.ExpiresAt.Sub(s.now())))
	defer expiryTimer.Stop()
	expiringTimer := time.NewTimer(maxDuration(0, sess.ExpiresAt.Add(-5*time.Minute).Sub(s.now())))
	defer expiringTimer.Stop()
	expiring := expiringTimer.C

	for {
		select {
		case <-r.Context().Done():
			return
		case err := <-readErrors:
			if errors.Is(err, errUnsupportedBrowserMessage) {
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "unsupported message"), time.Now().Add(time.Second))
			}
			return
		case <-expiryTimer.C:
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session expired"), time.Now().Add(time.Second))
			return
		case <-expiring:
			expiring = nil
			sequence++
			if err := s.writeBrowserEnvelope(conn, browserEnvelope{
				SchemaVersion: 1, InstanceID: s.instanceID, Sequence: sequence,
				Type: "session.expiring", StateRevision: s.events.Sequence(),
				SentAt:  s.now().UTC().Format(time.RFC3339Nano),
				Payload: map[string]any{"expiresAt": sess.ExpiresAt.UTC().Format(time.RFC3339)},
			}); err != nil {
				return
			}
		case <-pingTicker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		case control := <-controls:
			switch control.Type {
			case "ping":
				sequence++
				if err := s.writeBrowserEnvelope(conn, browserEnvelope{
					SchemaVersion: 1, InstanceID: s.instanceID, Sequence: sequence,
					Type: "pong", StateRevision: s.events.Sequence(),
					SentAt: s.now().UTC().Format(time.RFC3339Nano), Payload: map[string]any{},
				}); err != nil {
					return
				}
			case "subscribe", "unsubscribe", "resync":
				if err := s.writeBrowserSnapshot(r.Context(), conn, &sequence, s.events.Sequence()); err != nil {
					return
				}
			}
		case event := <-events:
			payload, revision, err := s.browserEventPayload(r.Context(), event)
			if err != nil {
				event.Type = "resync.required"
				event.Venue = ""
				payload = map[string]any{"reason": "state unavailable"}
				revision = event.Sequence
			}
			sequence++
			if err := s.writeBrowserEnvelope(conn, browserEnvelope{
				SchemaVersion: 1,
				InstanceID:    s.instanceID,
				Sequence:      sequence,
				Type:          event.Type,
				Venue:         event.Venue,
				AccountAlias:  s.accountAlias(event.Venue),
				StateRevision: revision,
				SentAt:        s.now().UTC().Format(time.RFC3339Nano),
				Payload:       payload,
			}); err != nil {
				return
			}
		}
	}
}

func (s *Server) writeBrowserSnapshot(ctx context.Context, conn *websocket.Conn, sequence *uint64, revision uint64) error {
	payload, err := s.browserSnapshot(ctx)
	if err != nil {
		return err
	}
	*sequence++
	return s.writeBrowserEnvelope(conn, browserEnvelope{
		SchemaVersion: 1,
		InstanceID:    s.instanceID,
		Sequence:      *sequence,
		Type:          "snapshot",
		StateRevision: revision,
		SentAt:        s.now().UTC().Format(time.RFC3339Nano),
		Payload:       payload,
	})
}

func (s *Server) browserSnapshot(ctx context.Context) (browserSnapshotPayload, error) {
	payload := browserSnapshotPayload{
		Accounts: make(map[domain.Venue]accountstate.Snapshot),
		Health:   s.publicStatuses(s.now()),
		Trades:   make([]publicTrade, 0),
	}
	if s.accounts != nil {
		for _, venue := range []domain.Venue{domain.VenueBybit, domain.VenueDeribit} {
			if _, enabled := s.enabledVenue(string(venue)); !enabled {
				continue
			}
			if snapshot, ok := s.accounts.Snapshot(venue); ok {
				payload.Accounts[venue] = snapshot
			}
		}
	}
	trades, err := s.trades.ListQuickTrades(ctx)
	if err != nil {
		return browserSnapshotPayload{}, err
	}
	if len(trades) > 25 {
		trades = trades[:25]
	}
	for _, trade := range trades {
		payload.Trades = append(payload.Trades, toPublicTrade(trade))
	}
	return payload, nil
}

func (s *Server) browserEventPayload(ctx context.Context, event runtimeevent.Event) (any, uint64, error) {
	switch event.Type {
	case "account.updated":
		if s.accounts == nil {
			return nil, event.Sequence, errors.New("account service unavailable")
		}
		snapshot, ok := s.accounts.Snapshot(event.Venue)
		if !ok {
			return nil, event.Sequence, errors.New("account snapshot unavailable")
		}
		return map[string]any{"account": snapshot}, event.Sequence, nil
	case "trade.updated":
		trade, err := s.trades.GetQuickTrade(ctx, event.IntentID)
		if err != nil {
			return nil, event.Sequence, err
		}
		return map[string]any{"trade": toPublicTrade(trade)}, event.Sequence, nil
	case "venue.health.updated":
		return map[string]any{"health": s.publicStatuses(s.now())}, event.Sequence, nil
	case "valuation.updated":
		return map[string]any{"venue": event.Venue}, event.Sequence, nil
	case "resync.required":
		return map[string]any{"reason": "subscriber overflow"}, event.Sequence, nil
	default:
		return map[string]any{}, event.Sequence, nil
	}
}

func (s *Server) accountAlias(venue domain.Venue) string {
	switch venue {
	case domain.VenueBybit:
		return s.base.AccountAlias
	case domain.VenueDeribit:
		return s.base.Deribit.AccountAlias
	default:
		return ""
	}
}

func (s *Server) writeBrowserEnvelope(conn *websocket.Conn, envelope browserEnvelope) error {
	if envelope.Payload == nil {
		envelope.Payload = map[string]any{}
	}
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(envelope)
}

func readBrowserSocket(conn *websocket.Conn, expiresAt time.Time, controls chan<- browserControl, errorsOut chan<- error) {
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			select {
			case errorsOut <- err:
			default:
			}
			return
		}
		_ = conn.SetReadDeadline(sessionReadDeadline(time.Now(), expiresAt))
		if messageType != websocket.TextMessage {
			select {
			case errorsOut <- errUnsupportedBrowserMessage:
			default:
			}
			return
		}
		var control browserControl
		if err := json.Unmarshal(payload, &control); err != nil {
			select {
			case errorsOut <- errUnsupportedBrowserMessage:
			default:
			}
			return
		}
		control.Type = strings.ToLower(strings.TrimSpace(control.Type))
		switch control.Type {
		case "ping", "subscribe", "unsubscribe", "resync":
		default:
			select {
			case errorsOut <- errUnsupportedBrowserMessage:
			default:
			}
			return
		}
		select {
		case controls <- control:
		default:
			select {
			case errorsOut <- errUnsupportedBrowserMessage:
			default:
			}
			return
		}
	}
}

func sessionReadDeadline(now, expiresAt time.Time) time.Time {
	deadline := now.Add(60 * time.Second)
	if expiresAt.Before(deadline) {
		return expiresAt
	}
	return deadline
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func browserInstanceID(secret []byte, startedAt time.Time) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(startedAt.UTC().Format(time.RFC3339Nano)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18])
}
