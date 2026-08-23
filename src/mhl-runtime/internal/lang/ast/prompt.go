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
type Prompt struct {
	Pos    lexer.Position
	Name   string   `parser:"'prompt' @Ident"`
	Params []*Param `parser:"'(' ( @@ ( ',' @@ )* )? ')'"`
	Body   *Expr    `parser:"'{' @@ '}'"`
}
