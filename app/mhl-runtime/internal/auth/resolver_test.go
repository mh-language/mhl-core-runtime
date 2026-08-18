package auth_test

import (
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/auth"
)

func TestResolveEnvAndRedact(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-secret")
	got, err := auth.Resolve(`env("ANTHROPIC_API_KEY")`)
	if err != nil || got != "sk-test-secret" {
		t.Fatalf("resolve = %q, %v", got, err)
	}
	if strings.Contains(auth.Redact("key="+got), got) {
		t.Fatal("resolved secret was not redacted")
	}
}

func TestResolveMissingFailsClosed(t *testing.T) {
	if _, err := auth.Resolve(`env("MHL_MISSING_CREDENTIAL")`); err == nil || !strings.Contains(err.Error(), "missing or empty") {
		t.Fatalf("expected clear missing credential error, got %v", err)
	}
}
