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

// refs maps a resolved secret value back to the credential reference it came
// from (e.g. `env("API_TOKEN")`), for the one value that equals it exactly.
// It lets checkpoint persistence store a re-resolvable reference instead of a
// dead `[REDACTED]` mask, so a `--resume` in a fresh process can recover the
// live value. Only whole-value references are tracked; a secret spliced into
// a larger string (a URL with an embedded password) has no reference and
// still falls back to masking.
var refs = struct {
	sync.RWMutex
	byValue map[string]string
}{byValue: map[string]string{}}

// ambiguousRef deliberately cannot resolve. It preserves ambiguity in a
// checkpoint even when it is loaded in another process with an empty registry.
const ambiguousRef = "ambiguous()"

// RememberRef records an explicitly identified credential, preserving its
// exact nonempty value regardless of length, whitespace or numeric shape.
// Distinct references for the same value remain ambiguous for this process.
func RememberRef(value, ref string) {
	if value == "" {
		return
	}
	remember(value)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return
	}
	refs.Lock()
	defer refs.Unlock()
	if prior, ok := refs.byValue[value]; ok && prior != ref {
		refs.byValue[value] = ambiguousRef
	} else {
		refs.byValue[value] = ref
	}
}

// RememberInferredRef applies the ordinary-environment false-positive guard
// before recording a reference. Explicit credentials must use RememberRef.
func RememberInferredRef(value, ref string) {
	if plausibleSecret(value) {
		RememberRef(value, ref)
	}
}

// RefFor returns a checkpoint reference for an exact value. If its provenance
// is ambiguous, it returns a marker that Resolve rejects explicitly on resume.
func RefFor(value string) (string, bool) {
	refs.RLock()
	defer refs.RUnlock()
	ref, ok := refs.byValue[value]
	return ref, ok
}

// Resolve resolves an env("KEY") reference and fails closed when it is
// missing or empty. Vault references are reserved for a future backend.
func Resolve(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == ambiguousRef {
		return "", fmt.Errorf("auth: ambiguous credential reference: distinct references resolved to the same value; start a new run with distinct credentials")
	}
	if strings.HasPrefix(ref, "env(\"") && strings.HasSuffix(ref, "\")") {
		key := strings.TrimSuffix(strings.TrimPrefix(ref, "env(\""), "\")")
		if key == "" || strings.ContainsAny(key, "\\\"") {
			return "", fmt.Errorf("auth: invalid environment reference %q", ref)
		}
		value, ok := os.LookupEnv(key)
		if !ok || value == "" {
			return "", fmt.Errorf("auth: environment variable %q is missing or empty", key)
		}
		RememberRef(value, fmt.Sprintf("env(%q)", key))
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
	if plausibleSecret(value) {
		remember(value)
	}
}

func plausibleSecret(value string) bool {
	value = strings.TrimSpace(value)
	if len([]rune(value)) < 6 {
		return false
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return false
	}
	switch strings.ToLower(value) {
	case "true", "false", "yes", "no", "null", "none":
		return false
	}
	return true
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
	// Replace only original input: a short explicit secret must not rewrite
	// the mask inserted for another secret.
	pairs := make([]string, 0, 2*len(secrets.values))
	for _, secret := range secrets.values {
		if secret != "" {
			pairs = append(pairs, secret, "[REDACTED]")
		}
	}
	return strings.NewReplacer(pairs...).Replace(value)
}
