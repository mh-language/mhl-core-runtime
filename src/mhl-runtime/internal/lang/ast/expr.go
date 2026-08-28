package ast

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
	Or   *OrExpr       `parser:"@@"`
	Tail []*CoalesceOp `parser:"@@*"`
}

// CoalesceOp is one `?? rhs` continuation of an Expr.
type CoalesceOp struct {
	Op  string  `parser:"@'??'"`
	Rhs *OrExpr `parser:"@@"`
}

// OrExpr handles the `||` logical-or operator.
type OrExpr struct {
	Head *AndExpr `parser:"@@"`
	Tail []*OrOp  `parser:"@@*"`
}

type OrOp struct {
	Op  string   `parser:"@'||'"`
	Rhs *AndExpr `parser:"@@"`
}

// AndExpr handles the `&&` logical-and operator.
type AndExpr struct {
	Head *EqExpr  `parser:"@@"`
	Tail []*AndOp `parser:"@@*"`
}

type AndOp struct {
	Op  string  `parser:"@'&&'"`
	Rhs *EqExpr `parser:"@@"`
}

// EqExpr handles equality operators `==` and `!=`.
type EqExpr struct {
	Head *CmpExpr `parser:"@@"`
	Tail []*EqOp  `parser:"@@*"`
}

type EqOp struct {
	Op  string   `parser:"@( '==' | '!=' )"`
	Rhs *CmpExpr `parser:"@@"`
}

// CmpExpr handles relational operators.
type CmpExpr struct {
	Head *AddExpr `parser:"@@"`
	Tail []*CmpOp `parser:"@@*"`
}

type CmpOp struct {
	Op  string   `parser:"@( '<=' | '>=' | '<' | '>' )"`
	Rhs *AddExpr `parser:"@@"`
}

// AddExpr handles additive operators `+` and `-`.
type AddExpr struct {
	Head *MulExpr `parser:"@@"`
	Tail []*AddOp `parser:"@@*"`
}

type AddOp struct {
	Op  string   `parser:"@( '+' | '-' )"`
	Rhs *MulExpr `parser:"@@"`
}

// MulExpr handles multiplicative operators `*`, `/`, and `%`.
type MulExpr struct {
	Head *Unary   `parser:"@@"`
	Tail []*MulOp `parser:"@@*"`
}

type MulOp struct {
	Op  string `parser:"@( '*' | '/' | '%' )"`
	Rhs *Unary `parser:"@@"`
}

// Unary handles prefix `!` and `-` operators.
type Unary struct {
	Op      string   `parser:"@( '!' | '-' )?"`
	Operand *Postfix `parser:"@@"`
}

// Postfix is a primary expression followed by any number of member-access
// (`.name`) or call (`(...)`) trailers.
type Postfix struct {
	Primary *Primary   `parser:"@@"`
	Ops     []*Trailer `parser:"@@*"`
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
// chaining): when the value to its left is `null`, or is an object that has
// no such field, the access yields `null` and the rest of the trailer chain
// is skipped instead of raising. Optional holds whether `?.` was written;
// Member carries the field name either way.
type Trailer struct {
	Optional bool   `parser:"( ( @'?.' | '.' )"`
	Member   string `parser:"    @Ident )"`
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

// Argument is a call argument, optionally named (`name: value`).
type Argument struct {
	Name  string `parser:"( @Ident ':' )?"`
	Value *Expr  `parser:"@@"`
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
	Duration string   `parser:"( @Duration"`
	Str      *string  `parser:"| @String"`
	MultiStr *string  `parser:"| @MLString"`
	Number   *float64 `parser:"| @Number"`
	Bool     *string  `parser:"| @( 'true' | 'false' )"`
	Null     bool     `parser:"| @'null'"`
	Object   *Object  `parser:"| @@"`
	Array    *Array   `parser:"| @@"`
	Agent    *Agent   `parser:"| @@"`
	Lambda   *Lambda  `parser:"| @@"`
	IfExpr   *IfExpr  `parser:"| @@"`
	Ident    string   `parser:"| @Ident"`
	Sub      *Expr    `parser:"| '(' @@ ')' )"`
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
// be a string or a bare identifier.
type ObjectField struct {
	KeyStr   *string `parser:"( @String"`
	KeyIdent *string `parser:"| @Ident )"`
	Value    *Expr   `parser:"':' @@"`
}

// Array is a bracket-delimited list literal.
type Array struct {
	Items []*Expr `parser:"'[' ( @@ ( ',' @@ )* ','? )? ']'"`
}
