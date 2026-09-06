package auth

import (
	"strings"
	"testing"
)

// These tests exercise process-wide registration with tiny secrets. Restore
// the registry so unrelated tests do not inherit intentionally broad masks.
func isolatedRegistry(t *testing.T) {
	t.Helper()
	secrets.Lock()
	oldValues := secrets.values
	secrets.values = nil
	secrets.Unlock()
	refs.Lock()
	oldRefs := refs.byValue
	refs.byValue = map[string]string{}
	refs.Unlock()
	t.Cleanup(func() {
		secrets.Lock()
		secrets.values = oldValues
		secrets.Unlock()
		refs.Lock()
		refs.byValue = oldRefs
		refs.Unlock()
	})
}

func TestExplicitCredentialsPreserveExactValues(t *testing.T) {
	for _, value := range []string{"x9!", "12345678", "true", "  padded-secret  ", " "} {
		t.Run(value, func(t *testing.T) {
			isolatedRegistry(t)
			t.Setenv("MHL_EXPLICIT", value)
			got, err := Resolve(`env("MHL_EXPLICIT")`)
			if err != nil || got != value {
				t.Fatalf("Resolve did not preserve value: %v", err)
			}
			if Redact(value) != "[REDACTED]" {
				t.Fatal("explicit value was not fully masked")
			}
			if ref, ok := RefFor(value); !ok || ref != `env("MHL_EXPLICIT")` {
				t.Fatal("exact credential reference lost")
			}
		})
	}
}

func TestCredentialReferenceCollisionFailsClosed(t *testing.T) {
	isolatedRegistry(t)
	const value = "shared-credential-material"
	RememberRef(value, `env("FIRST")`)
	RememberRef(value, `env("SECOND")`)
	RememberRef(value, `env("FIRST")`) // ambiguity must remain sticky
	ref, ok := RefFor(value)
	if !ok {
		t.Fatal("ambiguous credential must retain a fail-closed checkpoint marker")
	}
	if _, err := Resolve(ref); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want explicit ambiguity error, got %v", err)
	}
	if Redact(value) != "[REDACTED]" {
		t.Fatal("ambiguous secret was exposed")
	}
}

func TestInferredReferenceKeepsFalsePositiveGuard(t *testing.T) {
	isolatedRegistry(t)
	for _, value := range []string{"x9!", "12345678", "true", "false", "none", " "} {
		RememberInferredRef(value, `env("INFERRED_TOKEN")`)
		if Redact(value) != value {
			t.Fatal("heuristic registered ordinary value")
		}
		if _, ok := RefFor(value); ok {
			t.Fatal("heuristic recorded ordinary reference")
		}
	}
	value := "  inferred-secret-material  "
	RememberInferredRef(value, `env("INFERRED_TOKEN")`)
	if ref, ok := RefFor(value); !ok || ref != `env("INFERRED_TOKEN")` {
		t.Fatal("inferred reference lost whitespace")
	}
}

func TestRedactionDoesNotRewriteInsertedMask(t *testing.T) {
	isolatedRegistry(t)
	RememberRef("long-secret", `env("LONG")`)
	RememberRef("E", `env("SHORT")`)
	if Redact("long-secret E") != "[REDACTED] [REDACTED]" {
		t.Fatal("inserted mask was rewritten")
	}
}
