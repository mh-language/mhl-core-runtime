package mcp_test

import (
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/features/mcp"
	"github.com/yanjustino/mhl-runtime/internal/lang/parser"
)

// §3.3 declarations: one stdio server and one HTTP server with an env-resolved
// bearer token header.
const mcpSource = `
mcp_server PostgresDB {
    transport: "stdio"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-postgres", env("DATABASE_URL")]
}

mcp_server GitHubServer {
    transport: "http"
    url: "https://mcp.github.com/v1"
    headers: {
        "Authorization": "Bearer " + env("GITHUB_TOKEN")
    }
}
`

func TestBuildRegistryStdio(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/db")

	prog, err := parser.Parse(mcpSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	reg := mcp.BuildRegistry(prog)

	cfg, ok := reg.Get("PostgresDB")
	if !ok {
		t.Fatal("PostgresDB not in registry")
	}
	if cfg.Transport != mcp.TransportStdio {
		t.Errorf("transport = %q, want stdio", cfg.Transport)
	}
	if cfg.Command != "npx" {
		t.Errorf("command = %q, want npx", cfg.Command)
	}
	want := []string{"-y", "@modelcontextprotocol/server-postgres", "postgres://localhost/db"}
	if len(cfg.Args) != len(want) {
		t.Fatalf("args = %v, want %v", cfg.Args, want)
	}
	for i := range want {
		if cfg.Args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, cfg.Args[i], want[i])
		}
	}
}

// AC-5 (config layer): the Authorization header is resolved from
// "Bearer " + env("GITHUB_TOKEN").
func TestBuildRegistryHTTPHeaderResolution(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secret_123")

	prog, err := parser.Parse(mcpSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	reg := mcp.BuildRegistry(prog)

	cfg, ok := reg.Get("GitHubServer")
	if !ok {
		t.Fatal("GitHubServer not in registry")
	}
	if cfg.Transport != mcp.TransportHTTP {
		t.Errorf("transport = %q, want http", cfg.Transport)
	}
	if cfg.URL != "https://mcp.github.com/v1" {
		t.Errorf("url = %q", cfg.URL)
	}
	if got := cfg.Headers["Authorization"]; got != "Bearer ghp_secret_123" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer ghp_secret_123")
	}
}

func TestRegistryNames(t *testing.T) {
	prog, err := parser.Parse(mcpSource)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	reg := mcp.BuildRegistry(prog)
	names := reg.Names()
	if len(names) != 2 || names[0] != "GitHubServer" || names[1] != "PostgresDB" {
		t.Errorf("names = %v, want [GitHubServer PostgresDB]", names)
	}
}
