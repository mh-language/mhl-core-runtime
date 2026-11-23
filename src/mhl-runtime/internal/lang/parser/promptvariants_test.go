package parser

import (
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

func parsePrompt(t *testing.T, src string) *ast.Prompt {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range prog.Decls {
		if d.Prompt != nil {
			return d.Prompt
		}
	}
	t.Fatal("no prompt declared")
	return nil
}

// The two pre-existing body forms parse exactly as before: no variants.
func TestPromptVariantsLegacyFormsUnchanged(t *testing.T) {
	inline := parsePrompt(t, `prompt P(a: string) { "hi ${a}" }`)
	if inline.Variants != nil || inline.Body == nil || inline.Source != "" {
		t.Fatalf("inline prompt changed shape: %+v", inline)
	}
	if s, _ := ast.StringValue(inline.Body); s != "hi ${a}" {
		t.Fatalf("inline body = %q", s)
	}
	file := parsePrompt(t, `prompt P(a: string) from "p.md"`)
	if file.Variants != nil || file.Body != nil || file.Source != "p.md" {
		t.Fatalf("file prompt changed shape: %+v", file)
	}
	// `default` is a contextual keyword only: a body that is just an
	// identifier named default still parses as that expression.
	ident := parsePrompt(t, "prompt P() { default }")
	if ident.Variants != nil || ident.Body == nil {
		t.Fatalf("`{ default }` must stay an identifier body: %+v", ident)
	}
}

func TestPromptVariantsNormalized(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		wantBody   string // default inline body ("" when sourced)
		wantSource string
		wantArms   []string
	}{
		{"default only inline", `prompt P() { default { "d" } }`, "d", "", nil},
		{"default only file", `prompt P() { default from "d.md" }`, "", "d.md", nil},
		{"mixed arms", `prompt P(a: string) {
    default            from "d.md"
    for "claude/*"     from "c.md"
    for "cli/codex"    { "codex ${a}" }
}`, "", "d.md", []string{"claude/*", "cli/codex"}},
		{"default not first", `prompt P() {
    for "x" { "x" }
    default { "d" }
}`, "d", "", []string{"x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := parsePrompt(t, c.src)
			if c.wantBody != "" {
				if s, _ := ast.StringValue(p.Body); s != c.wantBody {
					t.Fatalf("body = %q, want %q", s, c.wantBody)
				}
			}
			if p.Source != c.wantSource {
				t.Fatalf("source = %q, want %q", p.Source, c.wantSource)
			}
			got := p.Targets()
			if strings.Join(got, ",") != strings.Join(c.wantArms, ",") || (c.wantArms == nil) != (p.Variants == nil) {
				t.Fatalf("arms = %v (variants %v), want %v", got, p.Variants, c.wantArms)
			}
			for _, v := range p.Variants {
				if v.Default {
					t.Fatal("the default arm must not stay in Variants")
				}
			}
		})
	}
}

func TestPromptVariantsErrors(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"no default":   {`prompt P() { for "x" { "x" } }`, "no default arm"},
		"two defaults": {`prompt P() { default { "a" } default { "b" } }`, "default arm twice"},
		"duplicate":    {`prompt P() { default { "a" } for "x" { "b" } for "x" { "c" } }`, `variant for "x" twice`},
		"star alone":   {`prompt P() { default { "a" } for "*" { "b" } }`, "write it as the default arm"},
		"star mid":     {`prompt P() { default { "a" } for "a*b" { "b" } }`, "only allowed once"},
		"two stars":    {`prompt P() { default { "a" } for "a**" { "b" } }`, "only allowed once"},
		"empty":        {`prompt P() { default { "a" } for "" { "b" } }`, "cannot be empty"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}
