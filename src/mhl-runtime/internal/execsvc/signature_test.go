package execsvc_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mhlruntime "github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// execsvc is the run path `mhl serve mcp` / `serve a2a` take — it never runs
// lint — so the checks lint makes for a typed signature must also hold here.

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunRefusesResultTypeWithoutOutput(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.mh": `
type Out = { ok: bool }
workflow W: Out {
    var ok = true
    step S { log("must not run") }
}
`})
	var out strings.Builder
	_, err := execsvc.Run(execsvc.Request{Source: filepath.Join(dir, "main.mh"), BaseDir: dir, Out: &out})
	if err == nil || !strings.Contains(err.Error(), "declares result type Out but no `output: { ... }` projection") {
		t.Fatalf("want the missing-projection error, got %v", err)
	}
	if strings.Contains(out.String(), "must not run") {
		t.Fatalf("the run must be refused before any step executes:\n%s", out.String())
	}
}

func TestRunRefusesSignatureOnTwoPartialFragments(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"main.mh": `
import "main.b.mh"
type In = { a: string }
partial workflow W(req: In) {
    entry step A { goto B }
}
`,
		"main.b.mh": `
type In2 = { b: string }
partial workflow W(req: In2) {
    step B { log("b") }
}
`,
	})
	_, err := execsvc.Run(execsvc.Request{Source: filepath.Join(dir, "main.mh"), BaseDir: dir, Inputs: map[string]any{"a": "x"}})
	if err == nil || !strings.Contains(err.Error(), "a typed signature is declared on more than one fragment") {
		t.Fatalf("want the duplicate-signature error, got %v", err)
	}
}

func TestRunOutputContractErrorIsTyped(t *testing.T) {
	dir := writeFiles(t, map[string]string{"main.mh": `
type Out = { ok: bool }
workflow W: Out {
    var result = { ok: 1 }
    step S { log("ran") }
    output: result
}
`})
	_, err := execsvc.Run(execsvc.Request{Source: filepath.Join(dir, "main.mh"), BaseDir: dir})
	var contract *mhlruntime.OutputContractError
	if !errors.As(err, &contract) || contract.Type != "Out" || contract.Pipeline != "W" {
		t.Fatalf("want *runtime.OutputContractError{Pipeline: W, Type: Out}, got %T %v", err, err)
	}
}
