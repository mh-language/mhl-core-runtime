package prompt_test

import (
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/prompt"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

func TestRenderForPicksTheVariant(t *testing.T) {
	prog, err := parser.Parse(`prompt P(x: string) {
    default         { "d ${x}" }
    for "claude/*"  { "c ${x}" }
    for "cli/codex" { "x ${x}" }
}`)
	if err != nil {
		t.Fatal(err)
	}
	decl := prog.Decls[0].Prompt
	args := map[string]string{"x": "1"}
	for target, want := range map[string]string{"": "d 1", "claude/opus": "c 1", "cli/codex": "x 1", "other": "d 1"} {
		got, err := prompt.RenderFor(decl, target, args)
		if err != nil || got != want {
			t.Errorf("RenderFor(%q) = %q, %v; want %q", target, got, err, want)
		}
	}
	if got, _ := prompt.Render(decl, args); got != "d 1" {
		t.Errorf("Render must stay the default, got %q", got)
	}
}

func TestVariantErrorsNameTheArm(t *testing.T) {
	prog, err := parser.Parse(`prompt P(x: string) {
    default        { "d ${x}" }
    for "claude/*" { "c ${y}" }
}`)
	if err != nil {
		t.Fatal(err)
	}
	decl := prog.Decls[0].Prompt
	if err := prompt.CheckBody(decl, nil); err != nil {
		t.Errorf("default arm is valid: %v", err)
	}
	err = prompt.CheckBody(decl, decl.Variants[0])
	if err == nil || !strings.Contains(err.Error(), `prompt "P" (for "claude/*")`) || !strings.Contains(err.Error(), `"y"`) {
		t.Errorf("CheckBody = %v, want the arm and the undeclared name", err)
	}
	if _, err := prompt.RenderFor(decl, "claude/x", map[string]string{"x": "1"}); err == nil || !strings.Contains(err.Error(), `(for "claude/*")`) {
		t.Errorf("RenderFor error = %v, want the arm named", err)
	}
}
