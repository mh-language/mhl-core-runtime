package ast

// Pipeline declares an execution pipeline: typed inputs, optional properties
// (e.g. checkpoint config), and ordered steps.
type Pipeline struct {
	Name string            `parser:"'pipeline' @Ident"`
	Body []*PipelineMember `parser:"'{' @@* '}'"`
}

// PipelineMember is one entry of a pipeline body.
type PipelineMember struct {
	Input *PipelineInput `parser:"( 'input' @@"`
	Step  *Step          `parser:"| @@"`
	Prop  *Property      `parser:"| @@ )"`
}

// PipelineInput is a typed pipeline input, e.g. `input issue_id: string`.
type PipelineInput struct {
	Name string `parser:"@Ident ':'"`
	Type string `parser:"@Ident"`
}

// Step is a named block of statements.
type Step struct {
	Name string       `parser:"'step' @Ident"`
	Body []*Statement `parser:"'{' @@* '}'"`
}

// Statement is a single statement inside a step body. Assignment is tried
// before a bare expression statement so that `x = expr` is not misread as an
// expression.
type Statement struct {
	Var    *VarDecl    `parser:"( @@"`
	If     *IfStmt     `parser:"| @@"`
	While  *WhileStmt  `parser:"| @@"`
	Try    *TryStmt    `parser:"| @@"`
	Assign *AssignStmt `parser:"| @@"`
	Expr   *ExprStmt   `parser:"| @@ )"`
}

// VarDecl declares and initializes a local variable: `var x = expr`.
type VarDecl struct {
	Name  string `parser:"'var' @Ident '='"`
	Value *Expr  `parser:"@@"`
}

// IfStmt is a conditional with an optional else block.
type IfStmt struct {
	Cond *Expr        `parser:"'if' '(' @@ ')'"`
	Then []*Statement `parser:"'{' @@* '}'"`
	Else []*Statement `parser:"( 'else' '{' @@* '}' )?"`
}

// WhileStmt is a conditional loop.
type WhileStmt struct {
	Cond *Expr        `parser:"'while' '(' @@ ')'"`
	Body []*Statement `parser:"'{' @@* '}'"`
}

// TryStmt is a try/catch(/finally) block.
type TryStmt struct {
	Body    []*Statement `parser:"'try' '{' @@* '}'"`
	ErrName string       `parser:"'catch' ( '(' @Ident ')' )?"`
	Catch   []*Statement `parser:"'{' @@* '}'"`
	Finally []*Statement `parser:"( 'finally' '{' @@* '}' )?"`
}

// AssignStmt assigns to an lvalue: `x = expr` or `obj.field = expr`.
type AssignStmt struct {
	Target *Postfix `parser:"@@"`
	Value  *Expr    `parser:"'=' @@"`
}

// ExprStmt is a bare expression used for its side effects, e.g. a call.
type ExprStmt struct {
	Expr *Expr `parser:"@@"`
}
