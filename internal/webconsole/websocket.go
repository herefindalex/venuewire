package webconsole

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

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

	conn.SetReadLimit(16 << 10)
	_ = conn.SetReadDeadline(sessionReadDeadline(s.now(), sess.ExpiresAt))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(sessionReadDeadline(s.now(), sess.ExpiresAt))
	})
	if err := conn.WriteJSON(map[string]any{
		"schemaVersion": 1,
		"instanceId":    "startup",
		"seq":           1,
		"type":          "session.ready",
		"sentAt":        s.now().UTC().Format(time.RFC3339Nano),
		"payload": map[string]any{
			"expiresAt": sess.ExpiresAt.UTC().Format(time.RFC3339),
			"readOnly":  !s.config.TradingEnabled,
		},
	}); err != nil {
		return
	}

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage || string(payload) != `{"type":"ping"}` {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "unsupported control message"), time.Now().Add(time.Second))
			return
		}
		if err := conn.WriteJSON(map[string]any{"schemaVersion": 1, "type": "pong", "sentAt": s.now().UTC().Format(time.RFC3339Nano)}); err != nil {
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
