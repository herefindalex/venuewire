package observability

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerRedactsSensitiveKeysAndKnownSecrets(t *testing.T) {
	secret := "unit-test-credential-value"
	var output bytes.Buffer
	logger := NewJSON(&output, secret)
	logger.Info("request using "+secret,
		slog.String("api_secret", secret),
		slog.String("X-BAPI-SIGN", "deadbeef"),
		slog.String("safe", "prefix-"+secret+"-suffix"),
		slog.Group("auth", slog.String("private_key", "key material")),
	)
	got := output.String()
	for _, forbidden := range []string{secret, "deadbeef", "key material"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("log leaked %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, redacted) {
		t.Fatalf("log did not contain redaction marker: %s", got)
	}
}

func TestLoggerRedactsPEMInMessage(t *testing.T) {
	var output bytes.Buffer
	logger := NewJSON(&output)
	pemMarker := "-----" + "BEGIN RSA " + "PRIVATE KEY----- material PRIVATE KEY-----"
	logger.Error(pemMarker)
	if strings.Contains(output.String(), "BEGIN RSA") {
		t.Fatalf("log leaked PEM material: %s", output.String())
	}
}

func TestLoggerRecursivelyRedactsAnyValues(t *testing.T) {
	secret := "nested-known-secret"
	var output bytes.Buffer
	logger := NewJSON(&output, secret)
	type authPayload struct {
		Signature string `json:"signature"`
		Note      string `json:"note"`
	}
	logger.Info("nested", slog.Any("payload", map[string]any{"auth": authPayload{Signature: "unknown-signature", Note: "contains " + secret}, "items": []any{map[string]any{"private_key": "material"}}}))
	got := output.String()
	for _, forbidden := range []string{"unknown-signature", secret, "material"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("nested log leaked %q: %s", forbidden, got)
		}
	}
}
