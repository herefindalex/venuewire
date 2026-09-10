package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// LoadEnvironment removes the global --env-file option from args and loads
// dotenv values which are not already present in the process environment.
// An explicitly empty OS value is intentionally preserved.
func LoadEnvironment(args []string) ([]string, error) {
	return loadEnvironment(args, os.Executable)
}

func loadEnvironment(args []string, executablePath func() (string, error)) ([]string, error) {
	path, explicit, remaining, err := envFileArgument(args)
	if err != nil {
		return nil, err
	}
	if explicit {
		if err := loadEnvironmentFiles([]string{path}, false); err != nil {
			return nil, err
		}
		return remaining, nil
	}

	executable, err := executablePath()
	if err != nil {
		return nil, fmt.Errorf("locate executable for default dotenv search: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	if err := loadEnvironmentFiles(defaultEnvironmentPaths(executable), true); err != nil {
		return nil, err
	}
	return remaining, nil
}

func defaultEnvironmentPaths(executable string) []string {
	binaryDirectory := filepath.Dir(executable)
	return []string{
		filepath.Join(binaryDirectory, ".env"),
		filepath.Join(filepath.Dir(binaryDirectory), ".env"),
	}
}

func loadEnvironmentFiles(paths []string, allowMissing bool) error {
	values := make(map[string]string)
	for _, path := range paths {
		fileValues, err := godotenv.Read(path)
		if err != nil {
			if allowMissing && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read dotenv file %q: %w", path, err)
		}
		for key, value := range fileValues {
			if _, exists := values[key]; !exists {
				values[key] = value
			}
		}
	}

	added := make([]string, 0, len(values))
	for key, value := range values {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			for _, addedKey := range added {
				_ = os.Unsetenv(addedKey)
			}
			return fmt.Errorf("load dotenv key %q: %w", key, err)
		}
		added = append(added, key)
	}
	return nil
}

func envFileArgument(args []string) (path string, explicit bool, remaining []string, err error) {
	remaining = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var candidate string
		switch {
		case arg == "--env-file":
			if i+1 >= len(args) {
				return "", false, nil, errors.New("--env-file requires a path")
			}
			i++
			candidate = args[i]
		case strings.HasPrefix(arg, "--env-file="):
			candidate = strings.TrimPrefix(arg, "--env-file=")
		default:
			remaining = append(remaining, arg)
			continue
		}
		if explicit {
			return "", false, nil, errors.New("--env-file may only be specified once")
		}
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return "", false, nil, errors.New("--env-file requires a non-empty path")
		}
		path, explicit = candidate, true
	}
	return path, explicit, remaining, nil
}
