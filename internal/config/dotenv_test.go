package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadEnvironmentPreservesOSValuesIncludingEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ENV")
	writeTestFile(t, path, "VENUEWIRE_DOTENV_WIN=file\nVENUEWIRE_DOTENV_EMPTY=file\nVENUEWIRE_DOTENV_ONLY='literal $VALUE # text'\n")
	t.Setenv("VENUEWIRE_DOTENV_WIN", "os")
	t.Setenv("VENUEWIRE_DOTENV_EMPTY", "")
	unsetAfterTest(t, "VENUEWIRE_DOTENV_ONLY")

	args, err := LoadEnvironment([]string{"--env-file", path, "--venue", "bybit", "time"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--venue", "bybit", "time"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	if got := os.Getenv("VENUEWIRE_DOTENV_WIN"); got != "os" {
		t.Fatalf("OS value overwritten: %q", got)
	}
	if got, ok := os.LookupEnv("VENUEWIRE_DOTENV_EMPTY"); !ok || got != "" {
		t.Fatalf("explicit empty OS value not preserved: %q, %v", got, ok)
	}
	if got := os.Getenv("VENUEWIRE_DOTENV_ONLY"); got != "literal $VALUE # text" {
		t.Fatalf("quoted literal parsed as %q", got)
	}
}

func TestLoadEnvironmentDefaultMissingIsAllowed(t *testing.T) {
	t.Chdir(t.TempDir())
	args, err := LoadEnvironment([]string{"help"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"help"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestLoadEnvironmentExplicitFileErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.env")
	writeTestFile(t, bad, "BROKEN='unterminated\n")

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing value", []string{"--env-file"}, "requires a path"},
		{"empty value", []string{"--env-file="}, "non-empty path"},
		{"duplicate", []string{"--env-file", bad, "--env-file=" + bad}, "only be specified once"},
		{"missing file", []string{"--env-file", filepath.Join(dir, "missing")}, "read dotenv file"},
		{"malformed file", []string{"--env-file", bad}, "read dotenv file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadEnvironment(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func unsetAfterTest(t *testing.T, key string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
