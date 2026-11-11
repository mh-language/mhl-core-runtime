package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A finding in an imported module is not placed at the module's line number
// inside the importing document: it goes on the first line, with the
// module's file and line in the message.
func TestDiagnosticsForImportedModuleFinding(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "agents.mh")
	if err := os.WriteFile(agents, []byte(`export agent Coder {
    input repo: string
    command: "copilot"
}

export tool Writer {
    generate(p: string): string -> Coder.run(prompt: p, repo: "r", modelo: "x")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.mh")
	text := "import { Writer } from \"agents.mh\"\n\nworkflow W {\n    step s {\n        log(Writer.generate(\"p\"))\n    }\n}\n"
	diags := diagnosticsFor(main, text)
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %+v", diags)
	}
	d := diags[0]
	if d.Range.Start.Line != 0 || !strings.HasPrefix(d.Message, agents+":7: ") || !strings.Contains(d.Message, `unknown argument "modelo"`) {
		t.Errorf("diagnostic = line %d %q, want line 0 prefixed with %s:7", d.Range.Start.Line, d.Message, agents)
	}
	// The module's own document gets it at its real line.
	src, _ := os.ReadFile(agents)
	own := diagnosticsFor(agents, string(src))
	if len(own) != 1 || own[0].Range.Start.Line != 6 || strings.HasPrefix(own[0].Message, agents) {
		t.Errorf("module's own diagnostics = %+v, want line 6 with a plain message", own)
	}
}
