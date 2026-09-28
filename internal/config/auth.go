package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// authFileMode / authDirMode are the permissions Section 3.10 requires for
// the user-level credential file. Anything looser gets a warning at startup.
const (
	authFileMode = 0o600
	authDirMode  = 0o700

	// commandTimeout bounds a !<command> secret-manager lookup so a hung
	// helper can't stall startup forever.
	commandTimeout = 10 * time.Second
)

// AuthConfig is the parsed auth.json: provider name -> literal key or
// "!<command>" to shell out to a secret manager. It exists as its own file
// precisely so config.json can be shared or committed without risk.
type AuthConfig map[string]string

// LoadAuth reads auth.json from the user-level dir. A missing file is not
// an error (the environment-variable fallback applies); a present but
// unreadable or malformed one is.
//
// The returned warnings describe permission problems; the caller prints
// them. The key values themselves are never included in a warning.
func LoadAuth(dir string) (AuthConfig, []string, error) {
	var warnings []string
	path := filepath.Join(dir, "auth.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, warnings, fmt.Errorf("read auth.json: %w", err)
	}
	checkAuthPerms(dir, path, &warnings)
	var auth AuthConfig
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, warnings, fmt.Errorf("parse auth.json: %w", err)
	}
	return auth, warnings, nil
}

func checkAuthPerms(dir, path string, warnings *[]string) {
	if fi, err := os.Stat(dir); err == nil && fi.Mode().Perm() != authDirMode {
		*warnings = append(*warnings, fmt.Sprintf(
			"directory %s has mode %v, expected 0%o; credentials may be readable by others",
			dir, fi.Mode().Perm(), authDirMode))
	}
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() != authFileMode {
		*warnings = append(*warnings, fmt.Sprintf(
			"%s has mode %v, expected 0%o; credentials may be readable by others",
			path, fi.Mode().Perm(), authFileMode))
	}
}

// Resolver resolves a provider's API key, in Section 3.10's order:
//
//  1. the user-level auth.json entry (literal, or "!<command>" shelled out
//     to the user's own secret manager — never honored from anywhere else)
//  2. the provider's environment variable
//
// Resolution never reads a project-level .tilde/ directory. RefusesToLoad
// reports whether one was found so the caller can say so: a project
// directory is exactly where a key gets committed by accident.
type Resolver struct {
	Auth        AuthConfig
	EnvProvider ProviderConfig

	cache map[string]ResolvedKey
}

type ResolvedKey struct {
	Value  string
	Source string // "auth.json", "auth.json command", or "environment"
}

// NewResolver builds a resolver. cache is populated lazily: a !<command>
// runs at most once per provider per process, per Section 3.10.
func NewResolver(auth AuthConfig, provider ProviderConfig) *Resolver {
	return &Resolver{Auth: auth, EnvProvider: provider, cache: map[string]ResolvedKey{}}
}

// RefusesToLoad returns the path of a project-level credential file found
// under projectDir, if any. tilde refuses to load it; the caller warns.
func RefusesToLoad(projectDir string) (string, bool) {
	if projectDir == "" {
		return "", false
	}
	path := filepath.Join(projectDir, ".tilde", "auth.json")
	if _, err := os.Stat(path); err == nil {
		return path, true
	}
	return "", false
}

// APIKey resolves the credential for the named provider. hasKey reports
// whether a key exists; for providers that need none (a local server with
// no api_key_env), hasKey false is not an error.
func (r *Resolver) APIKey(provider string) (key ResolvedKey, hasKey bool, err error) {
	if v, ok := r.Auth[provider]; ok && v != "" {
		if cached, ok := r.cache[provider]; ok {
			return cached, true, nil
		}
		if strings.HasPrefix(v, "!") {
			out, err := runKeyCommand(strings.TrimPrefix(v, "!"))
			if err != nil {
				return ResolvedKey{}, false,
					fmt.Errorf("auth.json command for %s failed: %w", provider, err)
			}
			key = ResolvedKey{Value: out, Source: "auth.json command"}
		} else {
			key = ResolvedKey{Value: v, Source: "auth.json"}
		}
		r.cache[provider] = key
		return key, true, nil
	}
	if r.EnvProvider.APIKeyEnv != "" {
		if v := os.Getenv(r.EnvProvider.APIKeyEnv); v != "" {
			return ResolvedKey{Value: v, Source: "environment"}, true, nil
		}
	}
	return ResolvedKey{}, false, nil
}

// runKeyCommand executes a secret-manager command and returns its trimmed
// stdout. Empty output, timeout, or nonzero exit leaves the key unresolved
// rather than silently falling back to something less secure.
func runKeyCommand(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timed out after %s", commandTimeout)
	}
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(out))
	if key == "" {
		return "", fmt.Errorf("produced no output")
	}
	return key, nil
}
