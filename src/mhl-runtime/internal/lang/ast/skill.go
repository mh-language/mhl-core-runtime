package ast

import "github.com/alecthomas/participle/v2/lexer"

// Skill declares a reusable instruction package loaded from a Markdown file
// with frontmatter (name/description required) — the mhl counterpart of
// Claude's Agent Skills:
//
//	skill CodeReview from "./skills/code-review/SKILL.md"
//
// Unlike Prompt, a skill has no inline-body alternative: it is always
// file-sourced. Name is the MHL identifier used inside the program (the
// same string nameof(CodeReview) resolves to) — distinct from
// Frontmatter["name"], which identifies the package in text sent to a
// model. Frontmatter and Content are nil/empty until import resolution
// loads and parses the file (internal/engine/interpreter/imports.go,
// internal/lang/lint/imports.go), mirroring how Prompt.Body is filled in
// from Prompt.Source.
type Skill struct {
	Pos         lexer.Position
	Name        string `parser:"'skill' @Ident"`
	Source      string `parser:"'from' @String"`
	Frontmatter map[string]any
	Content     string
}
