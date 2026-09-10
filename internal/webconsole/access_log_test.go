package webconsole

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLogCorrelatesRequestWithoutSensitiveValues(t *testing.T) {
	var logs bytes.Buffer
	server := newTestServer(t)
	server.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	request := validRequest(http.MethodPost, "/api/auth/login?token=query-secret", `{"username":"submitted-user","password":"body-secret"}`)
	request.Header.Set("Authorization", "Bearer authorization-secret")
	request.Header.Set("Cookie", "session=cookie-secret")
	response := performHTTPRequest(server, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d body=%s", response.Code, response.Body.String())
	}

	for _, secret := range []string{"query-secret", "submitted-user", "body-secret", "authorization-secret", "cookie-secret"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("access log exposed %q: %s", secret, logs.String())
		}
	}

	var accessEvent map[string]any
	scanner := bufio.NewScanner(strings.NewReader(logs.String()))
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode log event: %v", err)
		}
		if event["msg"] == "http request" {
			accessEvent = event
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if accessEvent == nil {
		t.Fatalf("missing access event: %s", logs.String())
	}
	if accessEvent["requestId"] == "" || accessEvent["method"] != http.MethodPost || accessEvent["path"] != "/api/auth/login" {
		t.Fatalf("access correlation fields = %#v", accessEvent)
	}
	if accessEvent["status"] != float64(http.StatusUnauthorized) || accessEvent["clientIp"] != "203.0.113.20" {
		t.Fatalf("access outcome fields = %#v", accessEvent)
	}
	if accessEvent["responseBytes"].(float64) <= 0 || accessEvent["durationMs"].(float64) < 0 {
		t.Fatalf("access measurement fields = %#v", accessEvent)
	}
}

func performHTTPRequest(server *Server, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
