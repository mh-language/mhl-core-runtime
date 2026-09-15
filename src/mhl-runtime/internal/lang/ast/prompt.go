package ast

import "github.com/alecthomas/participle/v2/lexer"

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
type Prompt struct {
	Pos         lexer.Position
	Name        string   `parser:"'prompt' @Ident"`
	Params      []*Param `parser:"'(' ( @@ ( ',' @@ )* )? ')'"`
	Body        *Expr    `parser:"( '{' @@ '}'"`
	Source      string   `parser:"| 'from' @String )"`
	Frontmatter map[string]any
}
