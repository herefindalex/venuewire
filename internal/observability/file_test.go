package observability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenLogFileCreatesPrivateAppendOnlyJSONTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log", "venuewire.log")
	file, err := OpenLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	logger := NewJSON(file, "fixture-secret")
	logger.Info("server fixture-secret started")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("log mode = %o", info.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "fixture-secret") || !strings.Contains(string(body), "[REDACTED]") {
		t.Fatalf("log redaction failed: %s", body)
	}

	file, err = OpenLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	NewJSON(file).Info("second event")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "server [REDACTED] started") || !strings.Contains(string(body), "second event") {
		t.Fatalf("log was not appended: %s", body)
	}
}
