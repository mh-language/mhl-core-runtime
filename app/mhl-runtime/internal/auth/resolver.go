// Package auth resolves credential references at the point of use and keeps
// resolved values out of diagnostic and persisted output.
package auth

import (
	"fmt"
	"os"
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
