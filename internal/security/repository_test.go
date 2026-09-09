package security

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRepositoryContainsNoCredentialAssignmentsOrPrivateKeys(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	assignment := regexp.MustCompile(`(?m)BYBIT_(?:API_KEY|API_SECRET|FIX_API_KEY)=[A-Za-z0-9_\-]{16,}`)
	privateKey := regexp.MustCompile(`BEGIN (?:RSA )?PRIVATE KEY`)
	specExamples := map[string]bool{
		"docs/05_TEST_PLAN.md":              true,
		"docs/06_SECURITY_OPERATIONS.md":    true,
		"docs/BYBIT_CONNECTOR_FULL_SPEC.md": true,
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, _ := filepath.Rel(root, path)
		if !entry.IsDir() && (entry.Name() == ".env" || entry.Name() == ".env~") {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".idea") {
			return filepath.SkipDir
		}
		if entry.IsDir() || specExamples[filepath.ToSlash(relative)] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if assignment.Match(data) {
			t.Errorf("possible Bybit credential assignment in %s", relative)
		}
		if privateKey.Match(data) {
			t.Errorf("possible private key in %s", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnvironmentExampleIsEmptyAndSecretsAreIgnored(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	file, err := os.Open(filepath.Join(root, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	allowed := map[string]string{"BYBIT_ENV": "testnet", "BYBIT_API_KEY": "", "BYBIT_API_SECRET": "", "BYBIT_FIX_API_KEY": "", "BYBIT_FIX_PRIVATE_KEY_PATH": ""}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			t.Errorf("malformed .env.example line %q", line)
			continue
		}
		if expected, ok := allowed[key]; !ok || value != expected {
			t.Errorf("unsafe .env.example line %q", line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{".env\n", ".env.*", ".env~", "*.pem", "*.key", "secrets/", "state/"} {
		if !strings.Contains(string(ignore), pattern) {
			t.Errorf(".gitignore missing %q", pattern)
		}
	}
}

func TestNoEnvironmentOrKeyFileIsTrackedWhenGitIsAvailable(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	command := exec.Command("git", "-C", root, "ls-files", "-z")
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Skip("workspace is not a git repository")
		}
		t.Fatal(err)
	}
	for _, raw := range strings.Split(string(output), "\x00") {
		name := filepath.Base(raw)
		if name == ".env" || name == ".env~" || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") {
			t.Errorf("sensitive file is tracked: %s", raw)
		}
	}
}
