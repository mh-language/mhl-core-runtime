package prompt_test

import (
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/features/prompt"
	"github.com/yanjustino/mhl-runtime/internal/lang/parser"
)

func TestRenderInterpolatesDeclaredParams(t *testing.T) {
	src := `
prompt SecurityAuditPrompt(file_path: string, code_content: string) {
    """
    Analyze '${file_path}':
    ${code_content}
    """
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decl := prog.Decls[0].Prompt

	got, err := prompt.Render(decl, map[string]string{
		"file_path":    "main.go",
		"code_content": "package main",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "main.go") || !strings.Contains(got, "package main") {
		t.Errorf("unexpected render: %q", got)
	}
}

func TestRenderSingleLineString(t *testing.T) {
	src := `
prompt Review(code: string) {
    "quick review of ${code}"
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decl := prog.Decls[0].Prompt

	got, err := prompt.Render(decl, map[string]string{"code": "x = 1"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "quick review of x = 1" {
		t.Errorf("got %q", got)
	}
}

func TestRenderMissingParamValue(t *testing.T) {
	src := `
prompt Review(code: string, lang: string) {
    "review ${code} in ${lang}"
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decl := prog.Decls[0].Prompt

	_, err = prompt.Render(decl, map[string]string{"code": "x = 1"})
	if err == nil {
		t.Fatal("expected an error for the missing 'lang' value")
	}
	if !strings.Contains(err.Error(), `missing value for parameter "lang"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRenderUnexpectedArgument(t *testing.T) {
	src := `
prompt Review(code: string) {
    "review ${code}"
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decl := prog.Decls[0].Prompt

	_, err = prompt.Render(decl, map[string]string{"code": "x", "extra": "y"})
	if err == nil {
		t.Fatal("expected an error for the unexpected 'extra' argument")
	}
	if !strings.Contains(err.Error(), `unexpected argument "extra"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRenderUndeclaredPlaceholder(t *testing.T) {
	src := `
prompt Review(code: string) {
    "review ${code} written by ${author}"
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decl := prog.Decls[0].Prompt

	_, err = prompt.Render(decl, map[string]string{"code": "x"})
	if err == nil {
		t.Fatal("expected an error for the undeclared 'author' placeholder")
	}
	if !strings.Contains(err.Error(), `undeclared parameter "author"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRenderEscapedPlaceholderStaysLiteral(t *testing.T) {
	src := `
prompt Review(code: string) {
    """
    review ${code}: run with \${code}
    """
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decl := prog.Decls[0].Prompt

	got, err := prompt.Render(decl, map[string]string{"code": "x = 1"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "review x = 1: run with ${code}" {
		t.Errorf("got %q", got)
	}
}

func TestRenderEscapedUndeclaredPlaceholderIsNotAnError(t *testing.T) {
	src := `
prompt Review(code: string) {
    """
    review ${code} — shell example: \${TARGET_DIR}
    """
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decl := prog.Decls[0].Prompt

	got, err := prompt.Render(decl, map[string]string{"code": "x"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "${TARGET_DIR}") {
		t.Errorf("got %q", got)
	}
}

func TestRenderNoParams(t *testing.T) {
	src := `
prompt Greeting() {
    "hello there"
}
`
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	decl := prog.Decls[0].Prompt

	got, err := prompt.Render(decl, map[string]string{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != "hello there" {
		t.Errorf("got %q", got)
	}
}
