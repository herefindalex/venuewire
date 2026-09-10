package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// LoadEnvironment removes the global --env-file option from args and loads
// dotenv values which are not already present in the process environment.
// An explicitly empty OS value is intentionally preserved.
func LoadEnvironment(args []string) ([]string, error) {
	path, explicit, remaining, err := envFileArgument(args)
	if err != nil {
		return nil, err
	}
	if path == "" {
		path = ".env"
	}

	values, err := godotenv.Read(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return remaining, nil
		}
		return nil, fmt.Errorf("read dotenv file %q: %w", path, err)
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
			return nil, fmt.Errorf("load dotenv key %q: %w", key, err)
		}
		added = append(added, key)
	}
	return remaining, nil
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
