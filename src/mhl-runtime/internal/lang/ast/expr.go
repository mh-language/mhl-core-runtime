package ast

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"
)

// Expr is the entry point of the expression grammar. Expressions appear both
// as property values (config) and inside pipeline statements. Precedence is
// encoded structurally, from lowest (null-coalescing `??`, then logical OR)
// to highest (unary/postfix).
//
// `??` binds looser than every other operator: `a || b ?? c` is
// `(a || b) ?? c`. Its right-hand side is only evaluated when the left is
// `null` (short-circuit), and — unlike `||`/`&&` — neither side has to be a
// bool. `Or` keeps its field name so the many call sites that reach straight
// for `expr.Or` still compile; `Tail` is empty for every expression that
// doesn't use `??`.
type Expr struct {
	Pos  lexer.Position
	Or   *OrExpr       `parser:"@@"`
	Tail []*CoalesceOp `parser:"@@*"`
}

// CoalesceOp is one `?? rhs` continuation of an Expr.
//
// Every operator literal in this file is pinned to the Punct token type
// (`'x':Punct`). Without that pin a bare `'-'` / `'!'` / ... in the grammar
// matches any token whose *value* is that string — including a String token
// once participle.Unquote has stripped its quotes, so `["a"].join("-")`
// would try to read the `"-"` argument as a unary-minus operator and fail
// with "unexpected token )". The pin makes these rules match punctuation
// only, never string content.
type CoalesceOp struct {
	Op  string  `parser:"@'??':Punct"`
	Rhs *OrExpr `parser:"@@"`
}

// OrExpr handles the `||` logical-or operator.
type OrExpr struct {
	Head *AndExpr `parser:"@@"`
	Tail []*OrOp  `parser:"@@*"`
}

type OrOp struct {
	Op  string   `parser:"@'||':Punct"`
	Rhs *AndExpr `parser:"@@"`
}

// AndExpr handles the `&&` logical-and operator.
type AndExpr struct {
	Head *EqExpr  `parser:"@@"`
	Tail []*AndOp `parser:"@@*"`
}

type AndOp struct {
	Op  string  `parser:"@'&&':Punct"`
	Rhs *EqExpr `parser:"@@"`
}

// EqExpr handles equality operators `==` and `!=`.
type EqExpr struct {
	Head *CmpExpr `parser:"@@"`
	Tail []*EqOp  `parser:"@@*"`
}

type EqOp struct {
	Op  string   `parser:"@( '==':Punct | '!=':Punct )"`
	Rhs *CmpExpr `parser:"@@"`
}

// CmpExpr handles relational operators.
type CmpExpr struct {
	Head *AddExpr `parser:"@@"`
	Tail []*CmpOp `parser:"@@*"`
}

type CmpOp struct {
	Op  string   `parser:"@( '<=':Punct | '>=':Punct | '<':Punct | '>':Punct )"`
	Rhs *AddExpr `parser:"@@"`
}

// AddExpr handles additive operators `+` and `-`.
type AddExpr struct {
	Head *MulExpr `parser:"@@"`
	Tail []*AddOp `parser:"@@*"`
}

type AddOp struct {
	Op  string   `parser:"@( '+':Punct | '-':Punct )"`
	Rhs *MulExpr `parser:"@@"`
}

// MulExpr handles multiplicative operators `*`, `/`, and `%`.
type MulExpr struct {
	Head *Unary   `parser:"@@"`
	Tail []*MulOp `parser:"@@*"`
}

type MulOp struct {
	Op  string `parser:"@( '*':Punct | '/':Punct | '%':Punct )"`
	Rhs *Unary `parser:"@@"`
}

// Unary handles prefix `!` and `-` operators. The `:Punct` pin is what keeps
// a string literal whose content happens to be exactly `-` or `!` — the
// classic `["a","b"].join("-")` — from being misread here as a prefix
// operator with a missing operand.
type Unary struct {
	Op      string   `parser:"@( '!':Punct | '-':Punct )?"`
	Operand *Postfix `parser:"@@"`
}

// Postfix is a primary expression followed by any number of member-access
// (`.name`) or call (`(...)`) trailers, then any number of `with { ... }`
// continuations (WithTail).
type Postfix struct {
	Primary  *Primary   `parser:"@@"`
	Ops      []*Trailer `parser:"@@*"`
	WithTail []*WithOp  `parser:"@@*" digest:"omitzero"`
}

// WithOp is one `obj with { field: value, ... }` continuation: a copy of the
// Postfix chain's value so far (value.DeepCopy — a ref-typed field keeps its
// shared identity, every other field is deep-copied) with Object's fields
// overridden on the copy, leaving the original value untouched. Chained
// `with`s (`a with {x:1} with {y:2}`) apply left to right, each on the
// previous result. `with` is contextual, exactly like `ref` (RefExpr): only
// `with {` starts one, so `with` stays usable as an identifier or argument
// name elsewhere — there is no bare-object-after-identifier construct
// anywhere else in the grammar (a call always requires `(...)`), so this can
// never collide with existing code. A field/index access on a `with` result
// needs parens (`(obj with {x:1}).field`) — WithTail is deliberately the
// last thing a Postfix can carry, not itself followed by more Ops, keeping
// the grammar (and DefinitionDigest's shape for every *other* Postfix,
// unaffected by this addition via digest:"omitzero") simple.
type WithOp struct {
	Pos    lexer.Position
	Object *Object `parser:"'with' @@"`
}

// Trailer is a member access, a call, an array slice, or an array index —
// `arr[0]` and `matrix[i][j]` (Ops repeating) parse unambiguously alongside
// the existing `.field` and `(...)` trailers since '[' can't start either of
// those. Slice is tried before Index — both start with '[' @@, but Slice
// requires a mandatory '..' that a plain index never has, so on input like
// `arr[3]` the Slice alternative fails to find '..' and backtracks
// (participle.UseLookahead(MaxLookahead), see internal/lang/parser/parser.go)
// to the plain Index alternative.
//
// A member access may be written `.name` (plain) or `?.name` (optional
// chaining): when the value to its left is `null`, is not an object at all
// (a string, number, ...), or is an object that has no such field, the
// access yields `null` and the rest of the trailer chain is skipped instead
// of raising. Optional holds whether `?.` was written; Member carries the
// field name either way.
//
// OptIndex is the dynamic-key twin, `?.[expr]` — the same null-on-absence
// behavior as `?.name` but with a runtime-computed key, covering an
// out-of-range array index and a missing object key. It is a read-only form
// (never an assignment target, unlike the plain Index trailer).
type Trailer struct {
	Optional bool   `parser:"( ( @'?.' | '.' )"`
	Member   string `parser:"    @Ident )"`
	OptIndex *Expr  `parser:"| '?.' '[' @@ ']'"`
	Call     *Call  `parser:"| @@"`
	Slice    *Slice `parser:"| '[' @@ ']'"`
	Index    *Expr  `parser:"| '[' @@ ']'"`
}

// SliceBound is one side of a slice range — `numbers[1..4]`'s `1` and `4`.
// An optional leading `^` marks the bound as counted from the end of the
// array instead of the start, e.g. `^3` in `numbers[^3..]` means "3 from
// the end" (size - 3).
type SliceBound struct {
	FromEnd bool  `parser:"@'^'?"`
	Value   *Expr `parser:"@@"`
}

// Slice is a range-index trailer body: `numbers[1..4]`, `numbers[3..]`,
// `numbers[..3]`, `numbers[^3..]`. Low and High are nil when their side of
// the '..' is omitted, meaning "from the start" and "to the end"
// respectively.
type Slice struct {
	Low  *SliceBound `parser:"@@?"`
	High *SliceBound `parser:"'..' @@?"`
}

// Call is an argument list applied to the preceding expression.
type Call struct {
	Args []*Argument `parser:"'(' ( @@ ( ',' @@ )* )? ')'"`
}

// Argument is a call argument, optionally named (`name: value`). A named
// argument with no value is shorthand for passing the variable of the same
// name: `f(project_id:)` is `f(project_id: project_id)`. Like an object
// field's shorthand, the parser expands it right after parsing
// (parser.expandShorthand), so Value is never nil on a parsed AST.
type Argument struct {
	Name  string `parser:"( @Ident ':'"`
	Value *Expr  `parser:"  @@? | @@ )"`
}

// Primary is an atomic expression. Lambda is tried before Sub since both
// start with "(" — a single-parameter lambda like "(item) -> ..." would
// otherwise be ambiguous with a parenthesized expression "(item)"; the
// disambiguator is the trailing "->" that only Lambda's rule requires, so
// the parser's backtracking (participle.UseLookahead(MaxLookahead), see
// internal/parser/parser.go) commits to Sub whenever that "->" isn't there.
// Zero-arg "()" and multi-arg "(a, b)" lambdas never collide with Sub at
// all, since Sub always holds exactly one Expr. IfExpr has no such
// ambiguity — its leading "if" keyword can't start any other alternative
// here — so it needs no special ordering beyond being tried before Ident
// (a bare `if` with no "(" after it, if that were ever meaningful, would
// fall through to Ident the same way `true`/`false`/`null` already do).
type Primary struct {
	Duration string     `parser:"( @Duration"`
	Str      *string    `parser:"| @String"`
	MultiStr *string    `parser:"| @MLString"`
	Number   *float64   `parser:"| @Number"`
	Bool     *string    `parser:"| @( 'true' | 'false' )"`
	Null     bool       `parser:"| @'null'"`
	Object   *Object    `parser:"| @@"`
	Array    *Array     `parser:"| @@"`
	Agent    *Agent     `parser:"| @@"`
	Lambda   *Lambda    `parser:"| @@"`
	IfExpr   *IfExpr    `parser:"| @@"`
	Match    *MatchExpr `parser:"| @@"`
	Ref      *RefExpr   `parser:"| @@" digest:"omitzero"`
	Ident    string     `parser:"| @Ident"`
	Sub      *Expr      `parser:"| '(' @@ ')' )"`
}

// RefExpr creates a reference object: `ref { name: "a" }` from an object
// literal, or `ref (expr)` from any expression that evaluates to an object
// (copied). Unlike a plain `{ ... }` — a value, copied whenever a variable
// holding it is read — a reference object is shared: every name holding it
// sees every mutation, and its identity survives a checkpoint/resume (see
// internal/engine/value). `ref` is contextual: only `ref {` / `ref (` start
// one, so `ref` stays usable as an identifier or argument name
// (`git.rev_parse(ref: "HEAD")`).
type RefExpr struct {
	Pos    lexer.Position
	Object *Object `parser:"'ref' ( @@"`
	Sub    *Expr   `parser:"| '(' @@ ')' )"`
}

// MatchExpr is an expression-position multi-way branch:
//
//	match status {
//	    Status.Draft     -> "not ready"
//	    Status.Published -> "live"
//	    _                -> "archived"
//	}
//
// Exactly one arm's Body is evaluated and returned. Arms are tried top to
// bottom: an arm matches when its Pattern evaluates to a value deep-equal
// to the subject (the same equality `==` uses), or when it is the `_`
// wildcard. A `match` with no matching arm and no `_` is a runtime error —
// `mhl lint` flags the statically-provable cases (a non-exhaustive `match`
// over an `enum` or `bool`) before that.
type MatchExpr struct {
	Pos     lexer.Position
	Subject *Expr       `parser:"'match' @@ '{'"`
	Arms    []*MatchArm `parser:"@@+ '}'"`
}

// MatchArm is one `pattern -> body` (or `_ -> body`) entry of a MatchExpr.
// Pattern is an ordinary expression evaluated at match time — an
// `Enum.Variant`, a bool/number/string literal, or any other expression
// whose value is compared against the subject.
type MatchArm struct {
	Pos      lexer.Position
	Wildcard bool  `parser:"( @'_'"`
	Pattern  *Expr `parser:"| @@ )"`
	Body     *Expr `parser:"'->' @@"`
}

// IfExpr is the ternary-like expression form of `if`, usable anywhere an
// expression can appear: `var result = if (cond) whenTrue else whenFalse`.
// Unlike IfStmt (pipeline.go) — which runs a block of statements for their
// side effects and has an optional `else` — both branches here are a single
// bare expression (no braces) and `else` is mandatory, since an expression
// must always evaluate to a value either way. This language has no notion
// of a braced block evaluating to a value, so that's deliberately not
// supported here.
type IfExpr struct {
	Cond *Expr `parser:"'if' '(' @@ ')'"`
	Then *Expr `parser:"@@"`
	Else *Expr `parser:"'else' @@"`
}

// Lambda is an anonymous function literal usable anywhere an expression can
// appear, e.g. `Linq.where(items, (item) -> item.passes == true)`. Like
// ToolMethod, its body is either a single expression or a full statement
// block with an optional `return` (void/nil if none) — Params reuses the
// same type ToolMethod already declares its parameters with.
type Lambda struct {
	Params []*Param     `parser:"'(' ( @@ ( ',' @@ )* )? ')' '->'"`
	Body   *Expr        `parser:"( @@"`
	Block  []*Statement `parser:"| '{' @@* '}' )"`
}

// Object is a brace-delimited map literal:
//
//	{ "Authorization": "Bearer " + env("X"), strict_mode: true }
type Object struct {
	// Fields may be separated by commas (inline literals) or by newlines
	// (multi-line config blocks), so the separating comma is optional.
	Fields []*ObjectField `parser:"'{' ( @@ ( ','? @@ )* ','? )? '}'"`
}

// ObjectField is a single `key: value` entry of an object literal. The key may
// be a string or a bare identifier. A bare identifier alone is shorthand for
// a field of the same name: `{artifact, path}` is `{artifact: artifact, path:
// path}`. The parser expands it right after parsing (parser.expandShorthand),
// so every consumer — and DefinitionDigest — only ever sees KeyIdent+Value;
// Shorthand is nil on any parsed AST.
type ObjectField struct {
	Pos       lexer.Position
	KeyStr    *string        `parser:"( ( @String"`
	KeyIdent  *string        `parser:"  | @Ident ) ':'"`
	Value     *Expr          `parser:"  @@"`
	Shorthand *ShorthandName `parser:"| @Ident )" digest:"omitzero"`
}

// ShorthandName is the identifier of a shorthand object field (`{name}`) or
// named argument (`f(name:)`). Its Capture rejects the statement keywords:
// keywords lex as plain identifiers, and without this a block body such as
// `-> { return x }` — where an expression is tried before a block — would
// parse as the object `{return: return, x: x}` instead of backtracking.
type ShorthandName string

// Capture implements participle.Capture.
func (n *ShorthandName) Capture(values []string) error {
	if shorthandReserved[values[0]] {
		return fmt.Errorf("%q is a keyword, not a shorthand field name", values[0])
	}
	*n = ShorthandName(values[0])
	return nil
}

var shorthandReserved = map[string]bool{
	"var": true, "const": true, "return": true, "break": true, "goto": true,
	"spawn": true, "wait": true, "if": true, "else": true, "while": true,
	"for": true, "in": true, "try": true, "catch": true, "finally": true,
	"match": true, "ref": true, "with": true, "true": true, "false": true,
	"null": true, "step": true, "input": true, "mem": true, "parallel": true,
	"route": true, "entry": true, "agent": true,
}

// Array is a bracket-delimited list literal.
type Array struct {
	Items []*Expr `parser:"'[' ( @@ ( ',' @@ )* ','? )? ']'"`
}
