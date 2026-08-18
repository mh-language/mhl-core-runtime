package mcp

import (
	"os"
	"sort"

	"github.com/yanjustino/mhl-runtime/internal/ast"
)

// Registry is a catalog of declared mcp_servers, resolved into ServerConfigs
// ready for the client to consume.
type Registry struct {
	servers map[string]ServerConfig
}

// BuildRegistry projects every mcp_server declaration in a parsed program onto
// a ServerConfig. String-valued fields that reference env("KEY") (including
// simple `"prefix" + env("KEY")` concatenations, as in the GitHubServer
// Authorization header) are resolved by consuming the current environment.
// Credential storage and vault backends are out of scope here (feature 5).
func BuildRegistry(prog *ast.Program) *Registry {
	r := &Registry{servers: map[string]ServerConfig{}}
	if prog == nil {
		return r
	}
	for _, d := range prog.Decls {
		if d == nil || d.MCPServer == nil {
			continue
		}
		r.servers[d.MCPServer.Name] = serverFromAST(d.MCPServer)
	}
	return r
}

// Get returns the resolved config for a named server.
func (r *Registry) Get(name string) (ServerConfig, bool) {
	cfg, ok := r.servers[name]
	return cfg, ok
}

// Names returns the declared server names in sorted order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.servers))
	for n := range r.servers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func serverFromAST(m *ast.MCPServer) ServerConfig {
	cfg := ServerConfig{Name: m.Name, Headers: map[string]string{}}
	for _, p := range m.Props {
		switch p.Name {
		case "transport":
			if v, ok := evalString(p.Value); ok {
				cfg.Transport = Transport(v)
			}
		case "command":
			if v, ok := evalString(p.Value); ok {
				cfg.Command = v
			}
		case "url":
			if v, ok := evalString(p.Value); ok {
				cfg.URL = v
			}
		case "args":
			cfg.Args = evalStringArray(p.Value)
		case "headers":
			cfg.Headers = evalStringObject(p.Value)
		}
	}
	return cfg
}

// evalStringArray flattens an array-literal into its string values.
func evalStringArray(e *ast.Expr) []string {
	p := bareArray(e)
	if p == nil {
		return nil
	}
	var out []string
	for _, item := range p.Items {
		if v, ok := evalString(item); ok {
			out = append(out, v)
		}
	}
	return out
}

// evalStringObject flattens an object-literal into a string->string map.
func evalStringObject(e *ast.Expr) map[string]string {
	obj := bareObject(e)
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
func evalString(e *ast.Expr) (string, bool) {
	if e == nil || e.Or == nil {
		return "", false
	}
	or := e.Or
	if len(or.Tail) != 0 || or.Head == nil {
		return "", false
	}
	and := or.Head
	if len(and.Tail) != 0 || and.Head == nil {
		return "", false
	}
	eq := and.Head
	if len(eq.Tail) != 0 || eq.Head == nil {
		return "", false
	}
	cmp := eq.Head
	if len(cmp.Tail) != 0 || cmp.Head == nil {
		return "", false
	}
	add := cmp.Head
	if add == nil || add.Head == nil {
		return "", false
	}
	s, ok := evalMulString(add.Head)
	if !ok {
		return "", false
	}
	for _, op := range add.Tail {
		if op.Op != "+" {
			return "", false
		}
		rs, ok := evalMulString(op.Rhs)
		if !ok {
			return "", false
		}
		s += rs
	}
	return s, true
}

func evalMulString(m *ast.MulExpr) (string, bool) {
	if m == nil || len(m.Tail) != 0 || m.Head == nil {
		return "", false
	}
	u := m.Head
	if u.Op != "" || u.Operand == nil {
		return "", false
	}
	return evalPostfixString(u.Operand)
}

func evalPostfixString(pf *ast.Postfix) (string, bool) {
	if pf == nil || pf.Primary == nil {
		return "", false
	}
	p := pf.Primary
	// env("KEY") resolves against the environment.
	if p.Ident == "env" && len(pf.Ops) == 1 && pf.Ops[0].Call != nil {
		args := pf.Ops[0].Call.Args
		if len(args) == 1 {
			if key, ok := literalString(args[0].Value); ok {
				return os.Getenv(key), true
			}
		}
		return "", false
	}
	if len(pf.Ops) != 0 {
		return "", false
	}
	return literalPrimary(p)
}

// literalString extracts a bare string literal from an expression.
func literalString(e *ast.Expr) (string, bool) {
	pf := barePostfix(e)
	if pf == nil {
		return "", false
	}
	return literalPrimary(pf.Primary)
}

func literalPrimary(p *ast.Primary) (string, bool) {
	if p == nil {
		return "", false
	}
	switch {
	case p.Str != nil:
		return *p.Str, true
	case p.MultiStr != nil:
		return *p.MultiStr, true
	}
	return "", false
}

// bareArray / bareObject / barePostfix unwrap an expression that is a single
// primary with no operators applied, returning the underlying node or nil.
func bareArray(e *ast.Expr) *ast.Array {
	if pf := barePostfix(e); pf != nil && len(pf.Ops) == 0 && pf.Primary != nil {
		return pf.Primary.Array
	}
	return nil
}

func bareObject(e *ast.Expr) *ast.Object {
	if pf := barePostfix(e); pf != nil && len(pf.Ops) == 0 && pf.Primary != nil {
		return pf.Primary.Object
	}
	return nil
}

func barePostfix(e *ast.Expr) *ast.Postfix {
	if e == nil || e.Or == nil {
		return nil
	}
	or := e.Or
	if len(or.Tail) != 0 || or.Head == nil {
		return nil
	}
	and := or.Head
	if len(and.Tail) != 0 || and.Head == nil {
		return nil
	}
	eq := and.Head
	if len(eq.Tail) != 0 || eq.Head == nil {
		return nil
	}
	cmp := eq.Head
	if len(cmp.Tail) != 0 || cmp.Head == nil {
		return nil
	}
	add := cmp.Head
	if len(add.Tail) != 0 || add.Head == nil {
		return nil
	}
	mul := add.Head
	if len(mul.Tail) != 0 || mul.Head == nil {
		return nil
	}
	u := mul.Head
	if u.Op != "" || u.Operand == nil {
		return nil
	}
	return u.Operand
}
