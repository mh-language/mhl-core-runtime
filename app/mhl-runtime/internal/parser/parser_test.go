package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// fixturesDir points at the §3 example fixtures relative to this package.
const fixturesDir = "../../test/fixtures"

// TestFixturesParse is the fixture-driven conformance suite (IC-1 / AC-1): it
// parses every §3 example block (3.1-3.6) and asserts zero parse errors, with
// a non-nil AST produced for each.
func TestFixturesParse(t *testing.T) {
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("reading fixtures dir: %v", err)
	}

	var mhlFiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".mhl" {
			mhlFiles = append(mhlFiles, e.Name())
		}
	}
	if len(mhlFiles) < 6 {
		t.Fatalf("expected at least 6 §3 fixtures, found %d", len(mhlFiles))
	}

	for _, name := range mhlFiles {
		name := name
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(fixturesDir, name))
			if err != nil {
				t.Fatalf("reading fixture %s: %v", name, err)
			}
			prog, err := Parse(string(src))
			if err != nil {
				t.Fatalf("expected zero parse errors for %s, got: %v", name, err)
			}
			if prog == nil {
				t.Fatalf("expected a non-nil AST for %s", name)
			}
			if len(prog.Decls) == 0 {
				t.Fatalf("expected at least one declaration in %s", name)
			}
		})
	}
}

// TestControlFlowParses guards RF-1's if/while/try-catch coverage using an
// inline pipeline snippet (try/catch is not present in the §3 examples).
func TestControlFlowParses(t *testing.T) {
	src := `
pipeline Flow {
    step Work {
        try {
            var x = 1
            if (x > 0) {
                x = x + 1
            } else {
                x = 0
            }
            while (x < 10) {
                x = x + 1
            }
        } catch (err) {
            log.write(err)
        } finally {
            cleanup()
        }
    }
}
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("expected control-flow snippet to parse, got: %v", err)
	}
	if prog == nil || len(prog.Decls) != 1 {
		t.Fatalf("expected exactly one pipeline declaration")
	}
}

// TestMalformedYieldsError is the failure path: a syntactically invalid .mhl
// source must yield a descriptive error rather than a partial/incorrect AST.
func TestMalformedYieldsError(t *testing.T) {
	// `mcp_server` requires a name identifier and a body; this is truncated.
	prog, err := Parse(`mcp_server {`)
	if err == nil {
		t.Fatalf("expected a parse error for malformed source, got nil")
	}
	if prog != nil {
		t.Fatalf("expected nil AST on parse error, got: %#v", prog)
	}
	if err.Error() == "" {
		t.Fatalf("expected a descriptive parse error message")
	}
}
