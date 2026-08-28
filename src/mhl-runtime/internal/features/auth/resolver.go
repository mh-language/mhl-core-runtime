// Package auth resolves credential references at the point of use and keeps
// resolved values out of diagnostic and persisted output.
package auth

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

var secrets = struct {
	sync.RWMutex
	values []string
}{}

// Resolve resolves an env("KEY") reference and fails closed when it is
// missing or empty. Vault references are reserved for a future backend.
func Resolve(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "env(\"") && strings.HasSuffix(ref, "\")") {
		key := strings.TrimSuffix(strings.TrimPrefix(ref, "env(\""), "\")")
		if key == "" || strings.ContainsAny(key, "\\\"") {
			return "", fmt.Errorf("auth: invalid environment reference %q", ref)
		}
		value, ok := os.LookupEnv(key)
		if !ok || value == "" {
			return "", fmt.Errorf("auth: environment variable %q is missing or empty", key)
		}
		remember(value)
		return value, nil
	}
	if strings.HasPrefix(ref, "vault(") {
		return "", fmt.Errorf("auth: vault reference %q cannot be resolved: no vault backend configured", ref)
	}
	return "", fmt.Errorf("auth: unsupported credential reference %q", ref)
}

func remember(value string) {
	secrets.Lock()
	defer secrets.Unlock()
	for _, known := range secrets.values {
		if known == value {
			return
		}
	}
	secrets.values = append(secrets.values, value)
}

// Register records a value discovered outside Resolve — an env("…") read
// through a secret-looking variable name, a bearer token or password handed
// to http.*, a proxy URL's embedded password — so Redact scrubs it too.
// Unlike Resolve (which trusts an explicit credential declaration), this
// path applies a false-positive guard: a value shorter than 6 characters,
// or one that is simply a number or a bool keyword, is ignored, because
// blanket-replacing "1" or "true" everywhere would corrupt ordinary output.
func Register(value string) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) < 6 {
		return
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return
	}
	switch strings.ToLower(value) {
	case "true", "false", "yes", "no", "null", "none":
		return
	}
	remember(value)
}

// LooksSecretName reports whether an environment-variable name is
// credential-shaped — the heuristic the env(...) builtin uses to decide
// whether to Register the value it just read. Deliberately conservative:
// it matches obvious secret words, and "key" only alongside a qualifier
// that rules out the many innocent "*_KEY" names (sort key, primary key).
func LooksSecretName(name string) bool {
	n := strings.ToUpper(name)
	for _, word := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "PASSPHRASE", "CREDENTIAL"} {
		if strings.Contains(n, word) {
			return true
		}
	}
	if strings.Contains(n, "KEY") {
		for _, q := range []string{"API", "ACCESS", "PRIVATE", "SECRET", "CLIENT", "ENCRYPT", "SIGNING"} {
			if strings.Contains(n, q) {
				return true
			}
		}
	}
	return false
}

// Redact replaces values resolved through Resolve with a stable mask.
func Redact(value string) string {
	secrets.RLock()
	defer secrets.RUnlock()
	for _, secret := range secrets.values {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}
