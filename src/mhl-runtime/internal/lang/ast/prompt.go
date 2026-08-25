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
// relative to the declaring file's directory the same way `use`/`import`
// paths are (internal/engine/interpreter/imports.go, internal/lang/lint/imports.go):
//
//	prompt SecurityAuditPrompt(file_path: string, code_content: string) from "./security-audit.prompt.md"
//
// Source holds that raw path and Body is nil until import resolution loads
// the file and rewrites Body to hold its (trimmed) contents as a multi-line
// string literal (ast.NewMultilineStringExpr) — from that point on, callers
// of Body (prompt.Render, lint's static prompt checks) can't tell the two
// forms apart.
type Prompt struct {
	Pos    lexer.Position
	Name   string   `parser:"'prompt' @Ident"`
	Params []*Param `parser:"'(' ( @@ ( ',' @@ )* )? ')'"`
	Body   *Expr    `parser:"( '{' @@ '}'"`
	Source string   `parser:"| 'from' @String )"`
}
