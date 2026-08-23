package ast

import "github.com/alecthomas/participle/v2/lexer"

// Pipeline declares an execution pipeline: typed inputs, optional properties
// (e.g. checkpoint config), and ordered steps. An optional leading `loop`
// keyword marks it as self-repeating: runtime.LoopRunner (not a plain
// runtime.Runner) drives it, reading its `stop_when`/`max_iterations` body
// Props the same generic way `checkpoint` is already read (see
// runtime.PipelineFromAST) — no separate grammar for them. A pipeline with
// no `loop` prefix still runs exactly once, unchanged.
type Pipeline struct {
	Loop bool              `parser:"@'loop'?"`
	Name string            `parser:"'pipeline' @Ident"`
	Body []*PipelineMember `parser:"'{' @@* '}'"`
}

// PipelineMember is one entry of a pipeline body. Var (reusing the same
// VarDecl a step body's `var x = expr` already is) declares a
// pipeline-scoped variable: evaluated once per run (see
// interpreter.EvalPipelineVars), then shared read/write across every step
// of that run via plain assignment (`x = expr`, no `var`) — unlike a
// step's own `var`, which never survives past that one step. 'var' is a
// hard disambiguator against Step ('step') and Prop (bare Ident ':'), so
// this adds no backtracking ambiguity.
type PipelineMember struct {
	Input *PipelineInput `parser:"( 'input' @@"`
	Var   *VarDecl       `parser:"| @@"`
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
// expression; Return is tried before Assign/Expr so `return ...` always
// commits to ReturnStmt rather than being misread as a bare `return`
// identifier expression followed by a second, unrelated statement.
type Statement struct {
	Pos    lexer.Position
	Var    *VarDecl    `parser:"( @@"`
	Return *ReturnStmt `parser:"| @@"`
	Break  *BreakStmt  `parser:"| @@"`
	Goto   *GotoStmt   `parser:"| @@"`
	If     *IfStmt     `parser:"| @@"`
	While  *WhileStmt  `parser:"| @@"`
	ForIn  *ForInStmt  `parser:"| @@"`
	Try    *TryStmt    `parser:"| @@"`
	Assign *AssignStmt `parser:"| @@"`
	Expr   *ExprStmt   `parser:"| @@ )"`
}

// VarDecl declares and initializes a local variable: `var x = expr`.
type VarDecl struct {
	Name  string `parser:"'var' @Ident '='"`
	Value *Expr  `parser:"@@"`
}

// ReturnStmt exits the enclosing tool method (or step) immediately, with an
// optional value — `return` alone is void, the same as a block that falls
// off its end with no return at all.
type ReturnStmt struct {
	Value *Expr `parser:"'return' @@?"`
}

// BreakStmt aborts the enclosing loop explicitly: it unwinds out of the
// current step, skips any remaining steps in this pipeline run, and signals
// the enclosing `loop` (loop.go) to stop outright rather than run another
// iteration. It never means "exit the nearest while/for-in" — mhl has no
// ordinary loop-exit statement, and reusing `break` for that too would give
// it two different meanings depending on where it's written; that stays a
// separate, later concern. Used in a pipeline that isn't wrapped by any
// `loop`, it still aborts that pipeline run — there's just no outer loop
// left to also stop. Reason is an optional expression carried into the
// run's terminal status (see runtime.BreakSignal).
type BreakStmt struct {
	Reason *Expr `parser:"'break' @@?"`
}

// GotoStmt transfers control to another named step in the same pipeline,
// abandoning the rest of the current step's body. Target may be any step,
// forward or backward, which is what a "recascade" (jump back to an earlier
// phase after a failed review) or a replan transition needs — the
// pipeline's step sequence is a small state machine (linear by default,
// explicitly overridable per statement), not a fixed array walk.
type GotoStmt struct {
	Target string `parser:"'goto' @Ident"`
}

// Every control-flow body below (IfStmt.Then/Else, WhileStmt.Body,
// ForInStmt.Body) accepts the same two shapes via `( '{' @@* '}' | @@ )`:
// either a brace-delimited block of statements, or — a concise inline form
// with no braces — a single bare statement, e.g. `if (cond) log("yes")` or
// `while (x < 10) x = x + 1`. Both branches capture into the same
// []*Statement field (a block of N statements, or a "block" of exactly
// one), so execBlock (internal/engine/interpreter/exec.go) runs either
// shape identically with no interpreter-side change needed.

// IfStmt is a conditional with an optional else block.
type IfStmt struct {
	Cond *Expr        `parser:"'if' '(' @@ ')'"`
	Then []*Statement `parser:"( '{' @@* '}' | @@ )"`
	Else []*Statement `parser:"( 'else' ( '{' @@* '}' | @@ ) )?"`
}

// WhileStmt is a conditional loop.
type WhileStmt struct {
	Cond *Expr        `parser:"'while' '(' @@ ')'"`
	Body []*Statement `parser:"( '{' @@* '}' | @@ )"`
}

// ForInStmt iterates an array, binding each element in turn to VarName —
// `for (var item in items) { ... }` or, inline, `for (var item in items)
// log(item)`. Scoped to arrays only for now (the same restriction
// internal/engine/interpreter's built-in size()/get_index() value methods
// already share); iterating an object's keys/values is a separate,
// not-yet-requested design decision.
type ForInStmt struct {
	VarName  string       `parser:"'for' '(' 'var' @Ident 'in'"`
	Iterable *Expr        `parser:"@@ ')'"`
	Body     []*Statement `parser:"( '{' @@* '}' | @@ )"`
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
