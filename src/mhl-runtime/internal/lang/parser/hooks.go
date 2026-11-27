package parser

import (
	"github.com/alecthomas/participle/v2/lexer"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// expandHooks rewrites every `on <event>[(param)] { ... }` member of a
// pipeline/workflow body (ast.PipelineHook) into the property the older
// spelling parses to — `<event>: (param) -> { ... }` — marked Property.On.
// A hook written without a parameter gets one named ast.HookParamOmitted,
// so the lambda always takes the one argument the runtime passes. With more
// than one parameter the lambda keeps only the first; Property.OnParams
// keeps them all for lint to report.
func expandHooks(prog *ast.Program) {
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		for _, m := range decl.Pipeline.Body {
			h := m.Hook
			if h == nil {
				continue
			}
			param := &ast.Param{Pos: h.Pos, Name: ast.HookParamOmitted}
			if len(h.Params) > 0 {
				param = h.Params[0]
			}
			m.Prop = &ast.Property{
				Pos:      h.Pos,
				EndPos:   h.EndPos,
				Name:     h.Event,
				Value:    lambdaExpr(&ast.Lambda{Params: []*ast.Param{param}, Block: h.Block}, h.Pos),
				On:       true,
				OnParams: h.Params,
			}
			m.Hook = nil
		}
	}
}

// lambdaExpr builds the expression for a bare lambda literal, at pos.
func lambdaExpr(l *ast.Lambda, pos lexer.Position) *ast.Expr {
	e := identExpr("", pos)
	p := ast.BarePostfix(e).Primary
	p.Ident = ""
	p.Lambda = l
	return e
}
