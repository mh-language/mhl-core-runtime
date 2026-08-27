package ast

import (
	"strconv"
	"strings"
	"time"
)

// This file reads a literal Go value straight out of an already-parsed
// *Expr — a string, number, bool, string array, object, or duration —
// for the case where that Expr is nothing but a single literal, with no
// operator applied. It exists for the two engine packages
// (internal/engine/interpreter, internal/engine/runtime) that read
// declaration properties (agent/memory/pipeline config, tool arguments)
// directly off the AST instead of through full expression evaluation, so
// they share one reading of what counts as "a bare literal" rather than
// keeping their own, independently-maintained copies.

// BarePostfix unwraps e down to its Postfix node when no binary or unary
// operator is applied along the way (i.e. e is nothing but a single
// literal/identifier/call chain); it returns nil otherwise.
func BarePostfix(e *Expr) *Postfix {
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

// barePrimary unwraps e down to its Primary when it is a bare literal with
// no trailing member-access/call trailers either.
func barePrimary(e *Expr) *Primary {
	pf := BarePostfix(e)
	if pf == nil || len(pf.Ops) != 0 || pf.Primary == nil {
		return nil
	}
	return pf.Primary
}

// BareObject returns the object literal of an expression that is a single
// primary with no operators or trailers applied, or nil.
func BareObject(e *Expr) *Object {
	if p := barePrimary(e); p != nil {
		return p.Object
	}
	return nil
}

// BareArray returns the array literal of an expression that is a single
// primary with no operators or trailers applied, or nil.
func BareArray(e *Expr) *Array {
	if p := barePrimary(e); p != nil {
		return p.Array
	}
	return nil
}

// BoolValue reads e as a bare `true`/`false` literal.
func BoolValue(e *Expr) (bool, bool) {
	p := barePrimary(e)
	if p == nil || p.Bool == nil {
		return false, false
	}
	return *p.Bool == "true", true
}

// StringValue reads e as a bare string or multi-line string literal.
func StringValue(e *Expr) (string, bool) {
	return StringFromPrimary(barePrimary(e))
}

// StringFromPrimary reads p as a string or multi-line string literal. It's
// the lower-level building block behind StringValue, exposed for a caller
// that has already unwrapped down to a *Primary itself (e.g. while walking
// a Postfix's trailers looking for something else, as
// internal/features/mcp's env()-aware string evaluator does).
func StringFromPrimary(p *Primary) (string, bool) {
	if p == nil {
		return "", false
	}
	switch {
	case p.Str != nil:
		return *p.Str, true
	case p.MultiStr != nil:
		return *p.MultiStr, true
	default:
		return "", false
	}
}

// NewMultilineStringExpr builds an *Expr that is nothing but s wrapped as a
// *Primary.MultiStr literal — the same shape StringValue reads off an
// inline """...""" body. It exists for a resolver that loads a prompt body
// from an external file (Prompt.Source, internal/engine/interpreter/imports.go,
// internal/lang/lint/imports.go) and needs to rewrite Prompt.Body so every
// downstream reader (prompt.Render, lint's static prompt checks) sees the
// loaded text exactly as it would see an inline literal, with no separate
// "where did this body come from" case to handle.
func NewMultilineStringExpr(s string) *Expr {
	return &Expr{Or: &OrExpr{Head: &AndExpr{Head: &EqExpr{Head: &CmpExpr{Head: &AddExpr{
		Head: &MulExpr{Head: &Unary{Operand: &Postfix{Primary: &Primary{MultiStr: &s}}}},
	}}}}}}
}

// IdentValue reads e as a bare identifier with no trailers or operators —
// e.g. an agent's bare-name entry in a `fallback: [...]` list, which names
// another declared agent rather than holding a literal value.
func IdentValue(e *Expr) (string, bool) {
	p := barePrimary(e)
	if p == nil || p.Ident == "" {
		return "", false
	}
	return p.Ident, true
}

// NumberValue reads e as a bare number literal.
func NumberValue(e *Expr) (float64, bool) {
	p := barePrimary(e)
	if p == nil || p.Number == nil {
		return 0, false
	}
	return *p.Number, true
}

// StringArrayValue reads e as a bare array literal whose every element is
// itself a bare string literal.
func StringArrayValue(e *Expr) ([]string, bool) {
	p := barePrimary(e)
	if p == nil || p.Array == nil {
		return nil, false
	}
	values := make([]string, 0, len(p.Array.Items))
	for _, item := range p.Array.Items {
		v, ok := StringValue(item)
		if !ok {
			return nil, false
		}
		values = append(values, v)
	}
	return values, true
}

// AgentValue reads e as a bare inline `agent { ... }` literal — the form
// used inside a `fallback: [...]` list (Primary.Agent).
func AgentValue(e *Expr) (*Agent, bool) {
	p := barePrimary(e)
	if p == nil || p.Agent == nil {
		return nil, false
	}
	return p.Agent, true
}

// DurationValue reads e as a bare duration literal (e.g. "120s", "7d").
func DurationValue(e *Expr) (time.Duration, bool) {
	p := barePrimary(e)
	if p == nil || p.Duration == "" {
		return 0, false
	}
	return ParseDuration(p.Duration)
}

// ParseDuration parses a MHL duration literal (e.g. "2s", "45s", "24h",
// "7d"). Go's time.ParseDuration has no day unit, so days are handled
// explicitly.
func ParseDuration(s string) (time.Duration, bool) {
	if len(s) < 2 {
		return 0, false
	}
	// "ms" is the one two-character unit the lexer's Duration pattern
	// accepts; every other unit is a single trailing letter.
	if strings.HasSuffix(s, "ms") {
		n, err := strconv.ParseFloat(s[:len(s)-2], 64)
		if err != nil {
			return 0, false
		}
		return time.Duration(n * float64(time.Millisecond)), true
	}
	unit := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, false
	}
	switch unit {
	case 's':
		return time.Duration(n * float64(time.Second)), true
	case 'm':
		return time.Duration(n * float64(time.Minute)), true
	case 'h':
		return time.Duration(n * float64(time.Hour)), true
	case 'd':
		return time.Duration(n * float64(24*time.Hour)), true
	}
	return 0, false
}
