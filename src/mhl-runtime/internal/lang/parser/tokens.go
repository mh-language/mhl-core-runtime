package parser

import "github.com/alecthomas/participle/v2/lexer"

// Token is one significant lexical token of a .mh source: comments and
// whitespace are dropped, exactly as the parser elides them. Kind is the
// lexer rule name ("Ident", "Punct", "String", "MLString", "Number",
// "Duration"); Offset is the byte offset of the token's first byte.
type Token struct {
	Kind   string
	Value  string
	Offset int
	Line   int
	Column int
}

// End is the byte offset just past the token.
func (t Token) End() int { return t.Offset + len(t.Value) }

// Tokens lexes source with the same lexer Parse uses and returns its
// significant tokens in order. It exists for tools that need to locate
// syntax the AST does not record (a brace, a `goto` target's exact
// position) — internal/lang/flow — without a second, drifting tokenizer:
// a `{` inside a string or an interpolation is part of a String token here,
// just as it is for the parser.
func Tokens(source string) ([]Token, error) {
	names := map[lexer.TokenType]string{}
	for name, typ := range mhlLexer.Symbols() {
		names[typ] = name
	}
	lx, err := mhlLexer.(lexer.StringDefinition).LexString("", stripLeadingDocBlock(source))
	if err != nil {
		return nil, err
	}
	var out []Token
	for {
		tok, err := lx.Next()
		if err != nil {
			return nil, err
		}
		if tok.EOF() {
			return out, nil
		}
		kind := names[tok.Type]
		if kind == "Comment" || kind == "Whitespace" {
			continue
		}
		out = append(out, Token{Kind: kind, Value: tok.Value, Offset: tok.Pos.Offset, Line: tok.Pos.Line, Column: tok.Pos.Column})
	}
}
