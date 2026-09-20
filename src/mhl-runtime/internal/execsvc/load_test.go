package execsvc_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// TestLoadDirectoryWithPartialFragmentDoesNotFailOtherWorkflows is the
// regression this exists for: Load scans every .mh file under dir
// independently (it's what `mhl serve mcp <dir>` uses to register one MCP
// tool per workflow), so a `partial` fragment sitting in that directory —
// resolved alone, its siblings never pulled in by anything in this
// particular file — legitimately has zero `entry` steps. That must not
// crash Load for the whole directory (it did, before Load learned to skip
// an incomplete partial fragment instead of registering-or-erroring on
// every Pipeline decl it finds): the primary file's own resolveImports
// pulls the fragment in via a whole-file `import`, merges it, and *that*
// declaration — complete, with its one entry step — is what should end up
// registered under "Discovery". A second, unrelated, ordinary pipeline in
// the same directory proves the fragment doesn't take the rest down with
// it.
func TestLoadDirectoryWithPartialFragmentDoesNotFailOtherWorkflows(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"fragment.mh": `
partial workflow Discovery {
    step Gate {
        log("gate reached")
    }
}
`,
		"main.mh": `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        log("dispatch reached")
        goto Gate
    }
}
`,
		"other.mh": `
pipeline Other {
    step S {
        log("other")
    }
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	workflows, err := execsvc.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(workflows) != 2 {
		t.Fatalf("expected 2 registered workflows (Discovery, Other), got %d: %+v", len(workflows), workflows)
	}
	discovery, ok := workflows["Discovery"]
	if !ok {
		t.Fatalf("expected \"Discovery\" to be registered, got %+v", workflows)
	}
	if got, want := discovery.Pipeline.Steps, []string{"Dispatch", "Gate"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Discovery.Pipeline.Steps = %v, want %v", got, want)
	}
	if _, ok := workflows["Other"]; !ok {
		t.Fatalf("expected \"Other\" to be registered too, got %+v", workflows)
	}
}
