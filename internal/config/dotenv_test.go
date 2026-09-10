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

	args, err := loadEnvironment([]string{"--env-file", path, "--venue", "bybit", "time"}, func() (string, error) {
		t.Fatal("explicit --env-file unexpectedly requested the executable path")
		return "", nil
	})
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
	executable := filepath.Join(t.TempDir(), "bin", "venuewire")
	args, err := loadEnvironment([]string{"help"}, func() (string, error) { return executable, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"help"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestDefaultEnvironmentSearchUsesBinaryDirectoryThenParent(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(bin, ".env"), "VENUEWIRE_BIN_ONLY=bin\nVENUEWIRE_SHARED=bin\n")
	writeTestFile(t, filepath.Join(root, ".env"), "VENUEWIRE_PARENT_ONLY=parent\nVENUEWIRE_SHARED=parent\n")
	for _, key := range []string{"VENUEWIRE_BIN_ONLY", "VENUEWIRE_PARENT_ONLY", "VENUEWIRE_SHARED"} {
		unsetAfterTest(t, key)
	}
	args, err := loadEnvironment([]string{"web"}, func() (string, error) { return filepath.Join(bin, "venuewire"), nil })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"web"}) {
		t.Fatalf("args = %#v", args)
	}
	if got := os.Getenv("VENUEWIRE_BIN_ONLY"); got != "bin" {
		t.Fatalf("binary-directory value = %q", got)
	}
	if got := os.Getenv("VENUEWIRE_PARENT_ONLY"); got != "parent" {
		t.Fatalf("parent-directory value = %q", got)
	}
	if got := os.Getenv("VENUEWIRE_SHARED"); got != "bin" {
		t.Fatalf("dotenv precedence = %q, want binary-directory value", got)
	}
}

func TestDefaultEnvironmentSearchIsAtomicWhenParentIsMalformed(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(bin, ".env"), "VENUEWIRE_ATOMIC=must-not-load\n")
	writeTestFile(t, filepath.Join(root, ".env"), "BROKEN='unterminated\n")
	unsetAfterTest(t, "VENUEWIRE_ATOMIC")
	if _, err := loadEnvironment(nil, func() (string, error) { return filepath.Join(bin, "venuewire"), nil }); err == nil {
		t.Fatal("malformed parent dotenv error = nil")
	}
	if _, exists := os.LookupEnv("VENUEWIRE_ATOMIC"); exists {
		t.Fatal("binary dotenv partially loaded before parent parse failure")
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
