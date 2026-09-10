package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const redacted = "[REDACTED]"

func OpenLogFile(path string) (*os.File, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("log path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure log file permissions: %w", err)
	}
	return file, nil
}

type redactingHandler struct {
	next    slog.Handler
	secrets []string
}

func NewJSON(w io.Writer, secrets ...string) *slog.Logger {
	filtered := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret != "" {
			filtered = append(filtered, secret)
		}
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(&redactingHandler{next: h, secrets: filtered})
}

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, h.cleanString(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		clean.AddAttrs(h.cleanAttr(attr))
		return true
	})
	return h.next.Handle(ctx, clean)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		clean[i] = h.cleanAttr(attr)
	}
	return &redactingHandler{next: h.next.WithAttrs(clean), secrets: h.secrets}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{next: h.next.WithGroup(name), secrets: h.secrets}
}

func (h *redactingHandler) cleanAttr(attr slog.Attr) slog.Attr {
	if sensitiveKey(attr.Key) {
		return slog.String(attr.Key, redacted)
	}
	if attr.Value.Kind() == slog.KindGroup {
		group := attr.Value.Group()
		for i := range group {
			group[i] = h.cleanAttr(group[i])
		}
		return slog.Group(attr.Key, attrsToAny(group)...)
	}
	if attr.Value.Kind() == slog.KindString {
		attr.Value = slog.StringValue(h.cleanString(attr.Value.String()))
	}
	if attr.Value.Kind() == slog.KindAny {
		value := attr.Value.Any()
		if err, ok := value.(error); ok {
			return slog.String(attr.Key, h.cleanString(err.Error()))
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return slog.String(attr.Key, redacted)
		}
		var generic any
		if err := json.Unmarshal(encoded, &generic); err != nil {
			return slog.String(attr.Key, redacted)
		}
		return slog.Any(attr.Key, h.cleanAny(generic))
	}
	return attr
}

func (h *redactingHandler) cleanAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveKey(key) {
				typed[key] = redacted
			} else {
				typed[key] = h.cleanAny(child)
			}
		}
		return typed
	case []any:
		for i := range typed {
			typed[i] = h.cleanAny(typed[i])
		}
		return typed
	case string:
		return h.cleanString(typed)
	default:
		return value
	}
}

func (h *redactingHandler) cleanString(value string) string {
	if strings.Contains(value, "-----BEGIN") && strings.Contains(value, "PRIVATE KEY-----") {
		return redacted
	}
	for _, secret := range h.secrets {
		value = strings.ReplaceAll(value, secret, redacted)
	}
	return value
}

func sensitiveKey(key string) bool {
	normalized := strings.NewReplacer("-", "", "_", "", ".", "").Replace(strings.ToLower(key))
	for _, token := range []string{"secret", "signature", "authorization", "privatekey", "rawdata", "xbapisign"} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}

func attrsToAny(attrs []slog.Attr) []any {
	values := make([]any, len(attrs))
	for i := range attrs {
		values[i] = attrs[i]
	}
	return values
}
