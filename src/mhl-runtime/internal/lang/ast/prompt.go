package ast

import (
	"fmt"
	"math"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
)

// Prompt declares a reusable, parameterized text template with ${param}
// interpolation (language-design.md §2 "Prompts Dinâmicos"):
//
//	prompt SecurityAuditPrompt(file_path: string, code_content: string) {
//	    """
//	    Analyze '${file_path}':
//	    ${code_content}
//	    """
//	}
//
// The body may instead be sourced from an external Markdown file, resolved
// relative to the declaring file's directory the same way `import`
// paths are (internal/engine/interpreter/imports.go, internal/lang/lint/imports.go):
//
//	prompt SecurityAuditPrompt(file_path: string, code_content: string) from "./security-audit.prompt.md"
//
// Source holds that raw path and Body is nil until import resolution loads
// the file and rewrites Body to hold its (trimmed) contents as a multi-line
// string literal (ast.NewMultilineStringExpr) — from that point on, callers
// of Body (prompt.Render, lint's static prompt checks) can't tell the two
// forms apart.
//
// A file-sourced prompt may optionally begin with a frontmatter block (the
// same flat "key: value" shape a skill's SKILL.md uses), parsed into
// Frontmatter and stripped out of Body — unlike a skill, no key is
// required, and a file with no such block leaves Frontmatter an empty map,
// Body unaffected. An inline `{ ... }` body never has frontmatter.
// PromptName.frontmatter (internal/engine/interpreter/eval.go) is how a
// program reads it back.
//
// A prompt may also declare variants of its body, one per harness/model
// target (docs/MHL-PROMPT-VARIANTS.md):
//
//	prompt Review(file: string) {
//	    default            from "review.md"
//	    for "claude/*"     from "review.claude.md"
//	    for "cli/codex"    { "Review ${file}." }
//	}
//
// The parser (parser.normalizePromptVariants) moves the mandatory `default`
// arm into Body/Source — so a variant prompt's default renders through every
// pre-existing code path unchanged, and a prompt without `for` arms hashes
// exactly like the plain form — and leaves only the `for` arms in Variants.
// Which arm is used is decided by the agent the prompt reaches
// (`prompt.target(t)`, Prompt.Variant), never by the prompt itself.
type Prompt struct {
	Pos         lexer.Position
	Name        string           `parser:"'prompt' @Ident"`
	Params      []*Param         `parser:"'(' ( @@ ( ',' @@ )* )? ')'"`
	Variants    []*PromptVariant `parser:"( '{' @@+ '}'" digest:"omitzero"`
	Body        *Expr            `parser:"| '{' @@ '}'"`
	Source      string           `parser:"| 'from' @String )"`
	Frontmatter map[string]any
}

// PromptVariant is one arm of a variant prompt body: `default <body>` or
// `for "<pattern>" <body>`, where <body> is `{ Expr }` or `from "path"`.
// After parsing, Prompt.Variants only ever holds `for` arms (Default false);
// the `default` arm lives in the Prompt's own Body/Source. A file-sourced
// arm's Source is loaded at import resolution exactly like Prompt.Source,
// rewriting Body and filling Frontmatter (which, unlike the default's, a
// program has no syntax to read back).
type PromptVariant struct {
	Pos         lexer.Position
	Default     bool   `parser:"( @'default'" digest:"-"`
	Pattern     string `parser:"| 'for' @String )"`
	Body        *Expr  `parser:"( '{' @@ '}'"`
	Source      string `parser:"| 'from' @String )"`
	Frontmatter map[string]any
}

// ValidPromptPattern reports why pattern cannot be a `for` arm's target
// pattern, or nil. A pattern is either an exact target ("cli/codex") or a
// prefix ending in a single trailing `*` ("claude/*", "ollama/qwen*"). A bare
// "*" is refused: it matches every target, which is what `default` is for.
func ValidPromptPattern(pattern string) error {
	switch {
	case pattern == "":
		return fmt.Errorf("a variant pattern cannot be empty")
	case pattern == "*":
		return fmt.Errorf(`variant pattern "*" matches every target; write it as the default arm`)
	case strings.Count(pattern, "*") > 1 || (strings.Contains(pattern, "*") && !strings.HasSuffix(pattern, "*")):
		return fmt.Errorf("variant pattern %q: `*` is only allowed once, as the last character", pattern)
	}
	return nil
}

// MatchPromptPattern reports whether target matches pattern and how
// specific the match is: an exact pattern scores math.MaxInt, a prefix
// pattern the length of its prefix. The `*` of a prefix pattern matches any
// suffix, `/` included.
func MatchPromptPattern(pattern, target string) (specificity int, ok bool) {
	if prefix, isPrefix := strings.CutSuffix(pattern, "*"); isPrefix {
		if strings.HasPrefix(target, prefix) {
			return len(prefix), true
		}
		return 0, false
	}
	if pattern == target {
		return math.MaxInt, true
	}
	return 0, false
}

// Variant picks the `for` arm target selects: an exact pattern beats any
// prefix, and a longer prefix beats a shorter one. Ties cannot happen, since
// two equally specific matching patterns are the same pattern and the parser
// refuses duplicates — so the order arms are declared in never matters. nil
// means the default arm (Body/Source), including for an empty target.
func (p *Prompt) Variant(target string) *PromptVariant {
	if target == "" {
		return nil
	}
	var best *PromptVariant
	bestScore := -1
	for _, v := range p.Variants {
		if score, ok := MatchPromptPattern(v.Pattern, target); ok && score > bestScore {
			best, bestScore = v, score
		}
	}
	return best
}

// Targets lists the `for` arm patterns in declaration order (nil when the
// prompt has no variants).
func (p *Prompt) Targets() []string {
	if len(p.Variants) == 0 {
		return nil
	}
	out := make([]string, len(p.Variants))
	for i, v := range p.Variants {
		out[i] = v.Pattern
	}
	return out
}
