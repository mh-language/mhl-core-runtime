package a2a_test

import (
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/a2a"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

const a2aSource = `
a2a_agent Translator {
    url: "https://translator.example.com/a2a"
    headers: {
        "Authorization": "Bearer " + env("A2A_TOKEN")
    }
    poll_interval: 2s
    poll_timeout: 30s
}

a2a_agent Summarizer {
    url: "https://summarizer.example.com/a2a"
}
`

func TestBuildRegistryResolvesConfig(t *testing.T) {
	t.Setenv("A2A_TOKEN", "sk_secret_123")

	prog, err := parser.Parse(a2aSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	reg := a2a.BuildRegistry(prog)

	cfg, ok := reg.Get("Translator")
	if !ok {
		t.Fatal("Translator not in registry")
	}
	if cfg.URL != "https://translator.example.com/a2a" {
		t.Errorf("url = %q", cfg.URL)
	}
	if got := cfg.Headers["Authorization"]; got != "Bearer sk_secret_123" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk_secret_123")
	}
	if cfg.PollInterval != 2*time.Second {
		t.Errorf("poll_interval = %s, want 2s", cfg.PollInterval)
	}
	if cfg.PollTimeout != 30*time.Second {
		t.Errorf("poll_timeout = %s, want 30s", cfg.PollTimeout)
	}
}

func TestBuildRegistryWithErrorFailsClosedOnMissingCredential(t *testing.T) {
	// A2A_TOKEN deliberately unset.
	prog, err := parser.Parse(a2aSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := a2a.BuildRegistryWithError(prog); err == nil {
		t.Fatal("expected BuildRegistryWithError to fail on unresolved env(\"A2A_TOKEN\")")
	}
}

func TestRegistryNames(t *testing.T) {
	t.Setenv("A2A_TOKEN", "x")
	prog, err := parser.Parse(a2aSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	names := a2a.BuildRegistry(prog).Names()
	if len(names) != 2 || names[0] != "Summarizer" || names[1] != "Translator" {
		t.Errorf("names = %v, want [Summarizer Translator]", names)
	}
}
