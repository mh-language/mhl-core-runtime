package a2a

import (
	"sort"

	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// Registry is a catalog of declared a2a_agents, resolved into Configs ready
// for the client to consume.
type Registry struct {
	agents map[string]Config
}

// BuildRegistry projects every a2a_agent declaration in a parsed program onto
// a Config. String-valued fields that reference env("KEY") (including simple
// `"prefix" + env("KEY")` concatenations, as in an Authorization header) are
// resolved by consuming the current environment.
func BuildRegistry(prog *ast.Program) *Registry {
	r := &Registry{agents: map[string]Config{}}
	if prog == nil {
		return r
	}
	for _, d := range prog.Decls {
		if d == nil || d.A2AAgent == nil {
			continue
		}
		r.agents[d.A2AAgent.Name] = configFromAST(d.A2AAgent)
	}
	return r
}

// BuildRegistryWithError is the fail-closed registry builder. It validates all
// credential references before returning a registry, so a missing credential
// cannot become an empty header.
func BuildRegistryWithError(prog *ast.Program) (*Registry, error) {
	if err := validateCredentials(prog); err != nil {
		return nil, err
	}
	return BuildRegistry(prog), nil
}

func validateCredentials(prog *ast.Program) error {
	if prog == nil {
		return nil
	}
	for _, d := range prog.Decls {
		if d == nil || d.A2AAgent == nil {
			continue
		}
		for _, p := range d.A2AAgent.Props {
			if p == nil {
				continue
			}
			if err := validateExpr(p.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateExpr resolves every env("KEY") reference reachable in e and returns
// the first resolution error. Unlike internal/features/mcp's equivalent it
// descends through `"prefix" + env("KEY")` concatenations too, so a bearer
// token in a `headers:` value — the common case — is covered by the
// fail-closed guarantee, not just a bare `env("KEY")`.
func validateExpr(e *ast.Expr) error {
	if e == nil {
		return nil
	}
	// A string-shaped expression (literal, env(...), or a concatenation of
	// those): evalString reports env-resolution failure as its error.
	if _, _, err := evalStringChecked(e); err != nil {
		return err
	}
	// Otherwise recurse into object/array literals to reach nested values.
	pf := ast.BarePostfix(e)
	if pf == nil || pf.Primary == nil {
		return nil
	}
	if pf.Primary.Array != nil {
		for _, item := range pf.Primary.Array.Items {
			if err := validateExpr(item); err != nil {
				return err
			}
		}
	}
	if pf.Primary.Object != nil {
		for _, field := range pf.Primary.Object.Fields {
			if err := validateExpr(field.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

// Get returns the resolved config for a named agent.
func (r *Registry) Get(name string) (Config, bool) {
	cfg, ok := r.agents[name]
	return cfg, ok
}

// Names returns the declared agent names in sorted order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.agents))
	for n := range r.agents {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func configFromAST(a *ast.A2AAgent) Config {
	cfg := Config{Name: a.Name, Headers: map[string]string{}}
	for _, p := range a.Props {
		switch p.Name {
		case "url":
			if v, ok := evalString(p.Value); ok {
				cfg.URL = v
			}
		case "headers":
			cfg.Headers = evalStringObject(p.Value)
		case "poll_interval":
			if d, ok := ast.DurationValue(p.Value); ok {
				cfg.PollInterval = d
			}
		case "poll_timeout":
			if d, ok := ast.DurationValue(p.Value); ok {
				cfg.PollTimeout = d
			}
		}
	}
	return cfg
}

// evalStringObject flattens an object-literal into a string->string map.
func evalStringObject(e *ast.Expr) map[string]string {
	obj := ast.BareObject(e)
	out := map[string]string{}
	if obj == nil {
		return out
	}
	for _, f := range obj.Fields {
		key := ""
		switch {
		case f.KeyStr != nil:
			key = *f.KeyStr
		case f.KeyIdent != nil:
			key = *f.KeyIdent
		}
		if key == "" {
			continue
		}
		if v, ok := evalString(f.Value); ok {
			out[key] = v
		}
	}
	return out
}

// evalString evaluates a string-valued expression: string literals, env("KEY")
// calls (resolved via the environment), and `a + b` concatenations thereof.
// It reports only whether e is such an expression that resolved cleanly; a
// failed env(...) resolution collapses to ("", false). Use evalStringChecked
// when the reason matters (fail-closed validation).
//
// Adapted from internal/features/mcp/registry.go — the two features share no
// code today and mhl favours a per-feature copy over a premature abstraction.
func evalString(e *ast.Expr) (string, bool) {
	s, ok, _ := evalStringChecked(e)
	return s, ok
}

// evalStringChecked is evalString with the env-resolution error surfaced. ok
// is true only when e is a string-shaped expression AND every env(...) in it
// resolved. When e is a string-shaped expression but an env(...) failed, ok
// is false and err is non-nil. When e is not string-shaped at all, ok is
// false and err is nil.
func evalStringChecked(e *ast.Expr) (string, bool, error) {
	if e == nil || e.Or == nil {
		return "", false, nil
	}
	or := e.Or
	if len(or.Tail) != 0 || or.Head == nil {
		return "", false, nil
	}
	and := or.Head
	if len(and.Tail) != 0 || and.Head == nil {
		return "", false, nil
	}
	eq := and.Head
	if len(eq.Tail) != 0 || eq.Head == nil {
		return "", false, nil
	}
	cmp := eq.Head
	if len(cmp.Tail) != 0 || cmp.Head == nil {
		return "", false, nil
	}
	add := cmp.Head
	if add == nil || add.Head == nil {
		return "", false, nil
	}
	s, ok, err := evalMulString(add.Head)
	if !ok {
		return "", false, err
	}
	for _, op := range add.Tail {
		if op.Op != "+" {
			return "", false, nil
		}
		rs, ok, err := evalMulString(op.Rhs)
		if !ok {
			return "", false, err
		}
		s += rs
	}
	return s, true, nil
}

func evalMulString(m *ast.MulExpr) (string, bool, error) {
	if m == nil || len(m.Tail) != 0 || m.Head == nil {
		return "", false, nil
	}
	u := m.Head
	if u.Op != "" || u.Operand == nil {
		return "", false, nil
	}
	return evalPostfixString(u.Operand)
}

func evalPostfixString(pf *ast.Postfix) (string, bool, error) {
	if pf == nil || pf.Primary == nil {
		return "", false, nil
	}
	p := pf.Primary
	// env("KEY") resolves against the environment.
	if p.Ident == "env" && len(pf.Ops) == 1 && pf.Ops[0].Call != nil {
		args := pf.Ops[0].Call.Args
		if len(args) == 1 {
			if key, ok := ast.StringValue(args[0].Value); ok {
				value, err := auth.Resolve(`env("` + key + `")`)
				if err != nil {
					return "", false, err
				}
				return value, true, nil
			}
		}
		return "", false, nil
	}
	if len(pf.Ops) != 0 {
		return "", false, nil
	}
	s, ok := ast.StringFromPrimary(p)
	return s, ok, nil
}
