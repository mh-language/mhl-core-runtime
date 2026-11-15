package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A finding inside an imported module is labelled with that module's file
// and line — not the importing file's name next to the module's line number —
// and `mhl lint dir` reports it once, however many files import the module.
func TestImportedModuleFindingNamesItsOwnFile(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "shared", "agents.mh")
	write(t, agents, `export agent Coder {
    input repo: string
    command: "copilot"
}

export tool Writer {
    generate(p: string): string -> Coder.run(prompt: p, repo: "r", modelo: "x")
}
`)
	for _, name := range []string{"a.mh", "b.mh"} {
		write(t, filepath.Join(dir, name), `import { Writer } from "shared/agents.mh"

workflow W {
    step s {
        log(Writer.generate("p"))
    }
}
`)
	}

	for _, entry := range []string{"a.mh", "b.mh"} {
		findings := lint.File(filepath.Join(dir, entry))
		if len(findings) != 1 {
			t.Fatalf("%s: want 1 finding, got %v", entry, findings)
		}
		if f := findings[0]; f.File != agents || f.Line != 7 || !strings.Contains(f.Message, `unknown argument "modelo"`) {
			t.Errorf("%s: finding = %s, want %s:7 unknown argument", entry, f, agents)
		}
	}

	all, err := lint.Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].File != agents || all[0].Line != 7 {
		t.Errorf("Dir: want the module's finding once, got %v", all)
	}
}
