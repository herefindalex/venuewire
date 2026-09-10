package webconsole

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"venuewire/internal/config"
	"venuewire/internal/intent"
)

const sessionCookieName = "__Host-trading_session"

type Server struct {
	config    config.WebConfig
	base      config.Config
	logger    *slog.Logger
	handler   http.Handler
	now       func() time.Time
	sessions  *sessionStore
	limiter   *loginLimiter
	trades    intent.Store
	rechecker TradeRechecker
	rechecks  *recheckCoordinator
	tradeApp  TradeApplication
	assets    fs.FS
}

type session struct {
	ID        string
	CSRFToken string
	CreatedAt time.Time
	ExpiresAt time.Time
	conns     map[*websocket.Conn]struct{}
}

type sessionStore struct {
	mu       sync.Mutex
	secret   []byte
	sessions map[string]*session
}

type loginAttempt struct {
	failures     []time.Time
	blockedUntil time.Time
}

type loginLimiter struct {
	mu                 sync.Mutex
	clients            map[string]*loginAttempt
	globalFailures     []time.Time
	globalBlockedUntil time.Time
	maxClients         int
}

type publicError struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

type contextKey string

const (
	requestIDKey contextKey = "request-id"
	clientIPKey  contextKey = "client-ip"
	sessionKey   contextKey = "session"
)

type Option func(*Server)

func WithTradeRechecker(rechecker TradeRechecker) Option {
	return func(server *Server) { server.rechecker = rechecker }
}

func WithTradeApplication(application TradeApplication) Option {
	return func(server *Server) { server.tradeApp = application }
}

func WithAssets(assets fs.FS) Option {
	return func(server *Server) { server.assets = assets }
}

func New(cfg config.WebConfig, base config.Config, logger *slog.Logger, options ...Option) *Server {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	s := &Server{
		config: cfg,
		base:   base,
		logger: logger,
		now:    time.Now,
		sessions: &sessionStore{
			secret:   append([]byte(nil), cfg.SessionSecret...),
			sessions: make(map[string]*session),
		},
		limiter:  &loginLimiter{clients: make(map[string]*loginAttempt), maxClients: 10_000},
		trades:   intent.Store{Path: base.IntentFile},
		rechecks: &recheckCoordinator{active: make(map[string]bool), last: make(map[string]time.Time)},
	}
	for _, option := range options {
		option(s)
	}
	s.handler = s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) ListenAndServe(ctx context.Context) error {
	server := &http.Server{
		Addr:              s.config.ListenAddress(),
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       75 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on configured web address: %w", err)
	}

	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.sessions.closeAll(websocket.CloseGoingAway, "server shutdown")
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown web server: %w", err)
		}
		return nil
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.Handle("GET /api/auth/me", s.requireSession(http.HandlerFunc(s.handleMe)))
	mux.Handle("POST /api/auth/logout", s.requireSession(s.requireCSRF(http.HandlerFunc(s.handleLogout))))
	mux.Handle("GET /api/venues", s.requireSession(http.HandlerFunc(s.handleVenues)))
	mux.Handle("GET /api/trades", s.requireSession(http.HandlerFunc(s.handleRecentTrades)))
	mux.Handle("GET /api/trades/{intentID}", s.requireSession(http.HandlerFunc(s.handleTradeDetail)))
	mux.Handle("POST /api/trades/{intentID}/recheck", s.requireSession(s.requireCSRF(http.HandlerFunc(s.handleTradeRecheck))))
	mux.Handle("POST /api/venues/{venue}/quotes", s.requireSession(s.requireCSRF(http.HandlerFunc(s.handleCreateQuote))))
	mux.Handle("POST /api/trades/confirm", s.requireSession(s.requireCSRF(http.HandlerFunc(s.handleConfirmTrade))))
	mux.Handle("GET /api/ws", s.requireSession(http.HandlerFunc(s.handleWebSocket)))
	mux.HandleFunc("/", s.handleFrontend)
	return s.securityHeaders(s.requestIdentity(s.proxyBoundary(mux)))
}

func (s *Server) requestIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID, err := randomToken(18)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be processed.", "")
			return
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID)))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) proxyBoundary(next http.Handler) http.Handler {
	expectedOrigin, _ := url.Parse(s.config.PublicOrigin)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directIP, err := remoteIP(r.RemoteAddr)
		if err != nil || !ipInNetworks(directIP, s.config.TrustedProxyCIDRs) {
			writeError(w, http.StatusForbidden, "untrusted_proxy", "The request was rejected.", requestID(r))
			return
		}
		if !strings.EqualFold(r.Host, expectedOrigin.Host) || r.Header.Get("X-Forwarded-Proto") != "https" {
			writeError(w, http.StatusForbidden, "invalid_request_origin", "The request was rejected.", requestID(r))
			return
		}
		forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
		realIP := strings.TrimSpace(r.Header.Get("X-Real-IP"))
		clientIP := net.ParseIP(forwarded)
		if clientIP == nil || strings.Contains(forwarded, ",") || realIP != forwarded {
			writeError(w, http.StatusForbidden, "invalid_forwarding_headers", "The request was rejected.", requestID(r))
			return
		}
		ctx := context.WithValue(r.Context(), clientIPKey, clientIP.String())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.validWriteOrigin(r) {
		writeError(w, http.StatusForbidden, "invalid_request_origin", "The request was rejected.", requestID(r))
		return
	}
	if !strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "A JSON request body is required.", requestID(r))
		return
	}
	clientIP, _ := r.Context().Value(clientIPKey).(string)
	now := s.now()
	if retryAfter, allowed := s.limiter.allow(clientIP, now); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(retryAfter.Round(time.Second)/time.Second))))
		writeError(w, http.StatusTooManyRequests, "login_rate_limited", "Too many sign-in attempts. Try again later.", requestID(r))
		return
	}

	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "The sign-in request is invalid.", requestID(r))
		return
	}
	if !constantTimeTextEqual(input.Username, s.config.Username) || !constantTimeTextEqual(input.Password, s.config.Password) {
		s.limiter.failure(clientIP, now)
		s.logger.Warn("login failed", slog.String("requestId", requestID(r)), slog.String("clientIp", clientIP))
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "The username or password is incorrect.", requestID(r))
		return
	}
	s.limiter.success(clientIP)

	sess, cookieValue, err := s.sessions.create(now, s.config.SessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be processed.", requestID(r))
		return
	}
	http.SetCookie(w, sessionCookie(cookieValue, sess.ExpiresAt, s.config.SessionTTL))
	writeJSON(w, http.StatusOK, map[string]any{
		"username":  s.config.Username,
		"csrfToken": sess.CSRFToken,
		"expiresAt": sess.ExpiresAt.UTC().Format(time.RFC3339),
		"readOnly":  !s.config.TradingEnabled,
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"username":  s.config.Username,
		"csrfToken": sess.CSRFToken,
		"expiresAt": sess.ExpiresAt.UTC().Format(time.RFC3339),
		"readOnly":  !s.config.TradingEnabled,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	s.sessions.delete(sess.ID, websocket.CloseNormalClosure, "signed out")
	http.SetCookie(w, expiredSessionCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleVenues(w http.ResponseWriter, _ *http.Request) {
	venues := make([]map[string]any, 0, 2)
	if s.config.BybitEnabled {
		venues = append(venues, map[string]any{"id": "bybit", "environment": "testnet", "accountAlias": s.base.AccountAlias})
	}
	if s.config.DeribitEnabled {
		venues = append(venues, map[string]any{"id": "deribit", "environment": "testnet", "accountAlias": s.base.Deribit.AccountAlias})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"venues":         venues,
		"defaultVenue":   s.config.DefaultVenue,
		"tradingEnabled": s.config.TradingEnabled,
	})
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication_required", "Sign in is required.", requestID(r))
			return
		}
		sess, ok := s.sessions.get(cookie.Value, s.now())
		if !ok {
			http.SetCookie(w, expiredSessionCookie())
			writeError(w, http.StatusUnauthorized, "session_expired", "The session is invalid or expired.", requestID(r))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}

func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := sessionFrom(r)
		if !s.validWriteOrigin(r) || !constantTimeTextEqual(r.Header.Get("X-CSRF-Token"), sess.CSRFToken) {
			writeError(w, http.StatusForbidden, "csrf_rejected", "The request was rejected.", requestID(r))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validWriteOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == s.config.PublicOrigin
}

func sessionFrom(r *http.Request) *session {
	sess, _ := r.Context().Value(sessionKey).(*session)
	return sess
}

func requestID(r *http.Request) string {
	value, _ := r.Context().Value(requestIDKey).(string)
	return value
}

func sessionCookie(value string, expires time.Time, ttl time.Duration) *http.Cookie {
	return &http.Cookie{Name: sessionCookieName, Value: value, Path: "/", Expires: expires, MaxAge: int(ttl / time.Second), Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

func expiredSessionCookie() *http.Cookie {
	return &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", Expires: time.Unix(1, 0), MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

func (s *sessionStore) create(now time.Time, ttl time.Duration) (*session, string, error) {
	id, err := randomToken(32)
	if err != nil {
		return nil, "", err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return nil, "", err
	}
	sess := &session{ID: id, CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(ttl), conns: make(map[*websocket.Conn]struct{})}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess, id + "." + s.sign(id), nil
}

func (s *sessionStore) get(cookieValue string, now time.Time) (*session, bool) {
	id, signature, ok := strings.Cut(cookieValue, ".")
	if !ok || !hmac.Equal([]byte(signature), []byte(s.sign(id))) {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok || !now.Before(sess.ExpiresAt) {
		if ok {
			delete(s.sessions, id)
			for conn := range sess.conns {
				_ = conn.Close()
			}
		}
		return nil, false
	}
	return sess, true
}

func (s *sessionStore) sign(id string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(id))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *sessionStore) delete(id string, code int, reason string) {
	s.mu.Lock()
	sess := s.sessions[id]
	delete(s.sessions, id)
	var conns []*websocket.Conn
	if sess != nil {
		conns = make([]*websocket.Conn, 0, len(sess.conns))
		for conn := range sess.conns {
			conns = append(conns, conn)
		}
		sess.conns = make(map[*websocket.Conn]struct{})
	}
	s.mu.Unlock()
	for _, conn := range conns {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
		_ = conn.Close()
	}
}

func (s *sessionStore) closeAll(code int, reason string) {
	s.mu.Lock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.delete(id, code, reason)
	}
}

func (s *sessionStore) addConn(id string, conn *websocket.Conn, maxConnections int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil || len(sess.conns) >= maxConnections {
		return false
	}
	sess.conns[conn] = struct{}{}
	return true
}

func (s *sessionStore) removeConn(id string, conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[id]; sess != nil {
		delete(sess.conns, conn)
	}
}

func (l *loginLimiter) allow(clientIP string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	if now.Before(l.globalBlockedUntil) {
		return l.globalBlockedUntil.Sub(now), false
	}
	if attempt := l.clients[clientIP]; attempt != nil && now.Before(attempt.blockedUntil) {
		return attempt.blockedUntil.Sub(now), false
	}
	return 0, true
}

func (l *loginLimiter) failure(clientIP string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	if len(l.clients) >= l.maxClients {
		l.globalBlockedUntil = now.Add(30 * time.Second)
		return
	}
	attempt := l.clients[clientIP]
	if attempt == nil {
		attempt = &loginAttempt{}
		l.clients[clientIP] = attempt
	}
	attempt.failures = append(attempt.failures, now)
	l.globalFailures = append(l.globalFailures, now)
	if len(attempt.failures) >= 5 {
		attempt.blockedUntil = now.Add(30 * time.Second)
	}
	if len(l.globalFailures) >= 200 {
		l.globalBlockedUntil = now.Add(30 * time.Second)
	}
}

func (l *loginLimiter) success(clientIP string) {
	l.mu.Lock()
	delete(l.clients, clientIP)
	l.mu.Unlock()
}

func (l *loginLimiter) prune(now time.Time) {
	cutoff := now.Add(-5 * time.Minute)
	l.globalFailures = recentTimes(l.globalFailures, cutoff)
	for clientIP, attempt := range l.clients {
		attempt.failures = recentTimes(attempt.failures, cutoff)
		if len(attempt.failures) == 0 && !now.Before(attempt.blockedUntil) {
			delete(l.clients, clientIP)
		}
	}
}

func recentTimes(values []time.Time, cutoff time.Time) []time.Time {
	index := 0
	for index < len(values) && values[index].Before(cutoff) {
		index++
	}
	return append(values[:0], values[index:]...)
}

func constantTimeTextEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

func randomToken(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func remoteIP(remoteAddr string) (net.IP, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, errors.New("remote address is not an IP")
	}
	return ip, nil
}

func ipInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func writeError(w http.ResponseWriter, status int, code, message, requestID string) {
	writeJSON(w, status, publicError{Error: code, Message: message, RequestID: requestID})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	s.logger.Error("web request failed", slog.String("operation", operation), slog.String("requestId", requestID(r)), slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be processed.", requestID(r))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
