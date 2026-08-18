package ast

// Expr is the entry point of the expression grammar. Expressions appear both
// as property values (config) and inside pipeline statements. Precedence is
// encoded structurally, from lowest (logical OR) to highest (unary/postfix).
type Expr struct {
	Or *OrExpr `parser:"@@"`
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

// MulExpr handles multiplicative operators `*` and `/`.
type MulExpr struct {
	Head *Unary   `parser:"@@"`
	Tail []*MulOp `parser:"@@*"`
}

type MulOp struct {
	Op  string `parser:"@( '*' | '/' )"`
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

// Trailer is either a member access or a call.
type Trailer struct {
	Member string `parser:"( '.' @Ident )"`
	Call   *Call  `parser:"| @@"`
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

// Primary is an atomic expression.
type Primary struct {
	Duration string   `parser:"( @Duration"`
	Str      *string  `parser:"| @String"`
	MultiStr *string  `parser:"| @MLString"`
	Number   *float64 `parser:"| @Number"`
	Bool     *string  `parser:"| @( 'true' | 'false' )"`
	Object   *Object  `parser:"| @@"`
	Array    *Array   `parser:"| @@"`
	Agent    *Agent   `parser:"| @@"`
	Ident    string   `parser:"| @Ident"`
	Sub      *Expr    `parser:"| '(' @@ ')' )"`
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
