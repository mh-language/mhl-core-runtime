package auth_test

import (
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
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

func TestRegisterMasksButGuardsFalsePositives(t *testing.T) {
	auth.Register("s3cret-token-material-9f2c")
	if got := auth.Redact("Authorization: Bearer s3cret-token-material-9f2c"); strings.Contains(got, "s3cret-token-material-9f2c") {
		t.Errorf("registered secret not masked: %q", got)
	}

	// Guard: too short, or a bare number / bool keyword, must NOT be
	// registered — masking "1" or "true" everywhere would wreck output.
	for _, noise := range []string{"1", "42", "true", "false", "abc", "3.14"} {
		auth.Register(noise)
		if auth.Redact("value="+noise) != "value="+noise {
			t.Errorf("guard failed: %q was registered", noise)
		}
	}
}

func TestLooksSecretName(t *testing.T) {
	secret := []string{"API_TOKEN", "github_token", "MY_CLIENT_SECRET", "DB_PASSWORD", "AWS_SECRET_ACCESS_KEY", "SIGNING_KEY"}
	plain := []string{"HOME", "PATH", "SORT_KEY", "PRIMARY_KEY", "ENABLE_LLM_CALL", "REGION"}
	for _, n := range secret {
		if !auth.LooksSecretName(n) {
			t.Errorf("LooksSecretName(%q) = false, want true", n)
		}
	}
	for _, n := range plain {
		if auth.LooksSecretName(n) {
			t.Errorf("LooksSecretName(%q) = true, want false", n)
		}
	}
}
