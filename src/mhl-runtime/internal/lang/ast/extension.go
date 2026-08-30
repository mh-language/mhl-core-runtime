package ast

import "github.com/alecthomas/participle/v2/lexer"

// Extension declares a use of a runtime extension: a capability (an MCP
// server, an A2A agent, or a third-party provider) resolved by `Kind` at run
// time rather than by a dedicated grammar rule. The body is a property bag,
// like Memory; the extension owning `Kind` interprets the properties and the
// methods called on `Name`.
//
//	extension mcp GitHub { transport: "http", url: "..." }
//	extension a2a Translator { url: "..." }
//	extension crm Customer { endpoint: env("CRM_URL") }
type Extension struct {
	Pos   lexer.Position
	Kind  string      `parser:"'extension' @Ident"`
	Name  string      `parser:"@Ident"`
	Props []*Property `parser:"'{' @@* '}'"`
}

// CredentialRefs returns every credential reference — an `env("KEY")` or
// `vault(...)` call — reachable anywhere in e: as the whole expression, an
// operand of an operator chain (`"Bearer " + env("TOKEN")`), an item of an
// array literal, a value in an object literal, a call argument, or a
// parenthesised sub-expression. Each reference is returned in its canonical
// `env("KEY")` form, de-duplicated, in first-seen order.
//
// It is a purely syntactic scan with no environment access; the caller
// resolves each reference through internal/features/auth. The interpreter
// uses it to fail closed on an unset credential in an extension declaration's
// properties before the extension is bound.
func CredentialRefs(e *Expr) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ref string) {
		if ref != "" && !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}

	var walkExpr func(*Expr)
	var walkPostfix func(*Postfix)

	walkExpr = func(x *Expr) {
		if x == nil || x.Or == nil {
			return
		}
		walkAnd := func(a *AndExpr) {
			if a == nil {
				return
			}
			// EqExpr -> CmpExpr -> AddExpr -> MulExpr -> Unary -> Postfix,
			// visiting each level's Head and every Tail Rhs.
			var walkAdd func(*AddExpr)
			walkMul := func(m *MulExpr) {
				if m == nil {
					return
				}
				if m.Head != nil {
					walkPostfix(m.Head.Operand)
				}
				for _, op := range m.Tail {
					if op.Rhs != nil {
						walkPostfix(op.Rhs.Operand)
					}
				}
			}
			walkAdd = func(ad *AddExpr) {
				if ad == nil {
					return
				}
				walkMul(ad.Head)
				for _, op := range ad.Tail {
					walkMul(op.Rhs)
				}
			}
			walkCmp := func(c *CmpExpr) {
				if c == nil {
					return
				}
				walkAdd(c.Head)
				for _, op := range c.Tail {
					walkAdd(op.Rhs)
				}
			}
			walkEq := func(eq *EqExpr) {
				if eq == nil {
					return
				}
				walkCmp(eq.Head)
				for _, op := range eq.Tail {
					walkCmp(op.Rhs)
				}
			}
			walkEq(a.Head)
			for _, op := range a.Tail {
				walkEq(op.Rhs)
			}
		}
		walkOr := func(o *OrExpr) {
			if o == nil {
				return
			}
			walkAnd(o.Head)
			for _, op := range o.Tail {
				walkAnd(op.Rhs)
			}
		}
		walkOr(x.Or)
		for _, op := range x.Tail {
			walkOr(op.Rhs)
		}
	}

	walkPostfix = func(pf *Postfix) {
		if pf == nil || pf.Primary == nil {
			return
		}
		p := pf.Primary
		// env("KEY") / vault("KEY") as `Primary Ident` + a single Call trailer.
		if (p.Ident == "env" || p.Ident == "vault") && len(pf.Ops) >= 1 && pf.Ops[0].Call != nil {
			args := pf.Ops[0].Call.Args
			if len(args) == 1 {
				if key, ok := StringValue(args[0].Value); ok {
					add(p.Ident + `("` + key + `")`)
				}
			}
		}
		// Recurse into every trailer's call arguments and index expressions.
		for _, op := range pf.Ops {
			if op.Call != nil {
				for _, a := range op.Call.Args {
					walkExpr(a.Value)
				}
			}
			if op.Index != nil {
				walkExpr(op.Index)
			}
			if op.OptIndex != nil {
				walkExpr(op.OptIndex)
			}
		}
		// Recurse into container / grouped primaries.
		if p.Sub != nil {
			walkExpr(p.Sub)
		}
		if p.Array != nil {
			for _, item := range p.Array.Items {
				walkExpr(item)
			}
		}
		if p.Object != nil {
			for _, f := range p.Object.Fields {
				walkExpr(f.Value)
			}
		}
	}

	walkExpr(e)
	return out
}

// AsExtension yields the (kind, name, props) view of an `extension`
// declaration. ok is false (with zero values) for a nil declaration or any
// other kind of declaration — callers use it to filter Decls without a type
// switch of their own.
func AsExtension(decl *Declaration) (kind, name string, props []*Property, ok bool) {
	if decl == nil || decl.Extension == nil {
		return "", "", nil, false
	}
	return decl.Extension.Kind, decl.Extension.Name, decl.Extension.Props, true
}
