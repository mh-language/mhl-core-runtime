package ast

import "github.com/alecthomas/participle/v2/lexer"

// Pipeline declares an execution pipeline: typed inputs, optional properties
// (e.g. checkpoint config), and ordered steps. An optional leading `loop`
// keyword marks it as self-repeating: runtime.LoopRunner (not a plain
// runtime.Runner) drives it, reading its `repeat { stop_when,
// max_iterations }` body Prop the same generic way `checkpoint { ... }` is
// already read (see runtime.PipelineFromAST) — no separate grammar for
// either. Named `repeat`, not `loop`, so it doesn't repeat the `loop`
// keyword that already precedes `pipeline`. A pipeline with no `loop` prefix
// still runs exactly once, unchanged.
//
// Kind is the declaration keyword: "pipeline" (steps run in order, each once
// — `goto` is a lint error) or "workflow" (identical execution model, but
// `goto <step>` is allowed, so the step sequence is an explicitly branching
// state machine). The distinction is purely static — the interpreter and
// runtime treat both the same; internal/lang/lint is what rejects `goto`
// outside a `workflow` (checkPipelineGoto).
type Pipeline struct {
	Loop bool              `parser:"@'loop'?"`
	Kind string            `parser:"@( 'pipeline' | 'workflow' )"`
	Name string            `parser:"@Ident"`
	Body []*PipelineMember `parser:"'{' @@* '}'"`
}

// IsWorkflow reports whether this declaration used the `workflow` keyword
// (rather than `pipeline`), which is what permits `goto` in its steps.
func (p *Pipeline) IsWorkflow() bool { return p.Kind == "workflow" }

// PipelineMember is one entry of a pipeline body. Var (reusing the same
// VarDecl a step body's `var x = expr` already is) declares a
// pipeline-scoped variable: evaluated once per run (see
// interpreter.EvalPipelineVars), then shared read/write across every step
// of that run via plain assignment (`x = expr`, no `var`) — unlike a
// step's own `var`, which never survives past that one step. Mem declares a
// pipeline-scoped variable backed by a persistent store instead of an
// in-process map: unlike Var, it is get-or-init (the initializer only runs
// the first time a given pipeline instance sees it) and survives across
// `loop pipeline` iterations and `--resume` — see
// interpreter.readMemVar/writeMemVar (memvar.go) and MemContext. 'var' and
// 'mem' are each a hard disambiguator against Step ('step') and Prop (bare
// Ident ':'), so this adds no backtracking ambiguity — and so is Parallel's
// leading 'parallel'. Prop also carries the named config blocks read by name
// in runtime.PipelineFromAST — 'checkpoint', 'spawn', 'repeat', and 'context'
// (the last opting the pipeline into the read-only `context.*` accessor its
// steps can read).
type PipelineMember struct {
	Input    *PipelineInput `parser:"( 'input' @@"`
	Const    *ConstDecl     `parser:"| @@"`
	Var      *VarDecl       `parser:"| @@"`
	Mem      *MemDecl       `parser:"| @@"`
	Parallel *ParallelGroup `parser:"| @@"`
	Step     *Step          `parser:"| @@"`
	Prop     *Property      `parser:"| @@ )"`
}

// ParallelGroup is a set of steps that run concurrently: the pipeline does
// not advance past the group until every step in it has finished (a
// barrier), then the group's variable writes are merged back into the
// pipeline's shared environment. Name is required — it is the group's
// identity in `mhl run` output and, crucially, in the resume checkpoint
// (runtime.Checkpoint.LastStep/NextStep may name a group, not just a step).
// A crash mid-group resumes by re-running the whole group, so branch steps
// must tolerate re-execution the same way any resumed step already does.
// `goto` may not target a step inside a group nor be used from within one,
// and `break` may not be used from within one — internal/lang/lint enforces
// all three (checkParallelGroups).
//
// An optional `timeout <duration>` header clause caps the whole barrier:
// runtime.Runner wraps the group's shared context with context.WithTimeout,
// so an expiry cancels every still-running branch at once and fails the
// group. A branch step may carry its own `timeout` too — the deadlines
// compose and the earliest one fires.
type ParallelGroup struct {
	Pos     lexer.Position
	Name    string  `parser:"'parallel' @Ident"`
	Timeout string  `parser:"( 'timeout' @Duration )?"`
	Steps   []*Step `parser:"'{' @@+ '}'"`
}

// MemDecl declares and initializes a pipeline-scoped persistent variable:
// `mem x = expr`. Grammar mirrors VarDecl exactly, just with the 'mem'
// keyword instead of 'var' — kept as a separate type (rather than reusing
// VarDecl) so PipelineMember.Mem and PipelineMember.Var are distinguishable
// without inspecting which keyword matched.
type MemDecl struct {
	Name  string `parser:"'mem' @Ident '='"`
	Value *Expr  `parser:"@@"`
}

// PipelineInput is a typed pipeline input, e.g. `input issue_id: string`.
type PipelineInput struct {
	Pos  lexer.Position
	Name string    `parser:"@Ident ':'"`
	Type *TypeExpr `parser:"@@"`
}

// Step is a named block of statements. An optional `timeout <duration>`
// header clause (`step Build timeout 3m { ... }`) caps how long the step may
// run: runtime.Runner wraps the step's context with context.WithTimeout, and
// an expiry fails the step the same way an explicit fail() would — a
// checkpoint is written when checkpointing is enabled, so `run/resume`
// re-enters the step with a fresh budget. A step literally named `timeout`
// cannot also carry the clause; the word is reserved in that position, like
// `parallel`/`any`/`of`.
type Step struct {
	Pos     lexer.Position
	Name    string       `parser:"'step' @Ident"`
	Timeout string       `parser:"( 'timeout' @Duration )?"`
	Body    []*Statement `parser:"'{' @@* '}'"`
}

// Statement is a single statement inside a step body. Assignment is tried
// before a bare expression statement so that `x = expr` is not misread as an
// expression; Return is tried before Assign/Expr so `return ...` always
// commits to ReturnStmt rather than being misread as a bare `return`
// identifier expression followed by a second, unrelated statement.
type Statement struct {
	Pos    lexer.Position
	Const  *ConstDecl  `parser:"( @@"`
	Var    *VarDecl    `parser:"| @@"`
	Return *ReturnStmt `parser:"| @@"`
	Break  *BreakStmt  `parser:"| @@"`
	Goto   *GotoStmt   `parser:"| @@"`
	Spawn  *SpawnStmt  `parser:"| @@"`
	Wait   *WaitStmt   `parser:"| @@"`
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

// ConstDecl declares a single-assignment binding: `const x = expr`. Grammar
// mirrors VarDecl with the `const` keyword. Reassigning the name (`x = ...`
// or `x += ...`) is an error at both `mhl run` and `mhl lint`. Re-executing
// the *declaration* itself — e.g. a `const` inside a `while` body on a later
// iteration — simply rebinds; only assignment to the name is forbidden.
type ConstDecl struct {
	Pos   lexer.Position
	Name  string `parser:"'const' @Ident '='"`
	Value *Expr  `parser:"@@"`
}

// ReturnStmt exits the enclosing tool method (or step) immediately, with an
// optional value — `return` alone is void, the same as a block that falls
// off its end with no return at all.
type ReturnStmt struct {
	Value *Expr `parser:"'return' @@?"`
}

// BreakStmt ends the enclosing loop early — a clean stop, exit 0, not a
// failure: it leaves the current step, skips the remaining steps of this
// run, and tells the enclosing `loop` (loop.go) to stop instead of running
// another iteration. The variable state built up so far is kept as the
// run's result (result.json / context.vars), the same as a normal
// completion. It never means "exit the nearest while/for-in" — mhl has no
// ordinary loop-exit statement, and reusing `break` for that too would give
// it two different meanings depending on where it's written; that stays a
// separate, later concern. In a pipeline that isn't wrapped by any `loop`,
// it just ends that run early the same way. Reason is an optional expression
// carried into the run's terminal status (see runtime.BreakSignal).
type BreakStmt struct {
	Reason *Expr `parser:"'break' @@?"`
}

// SpawnStmt starts an agent call on a background goroutine and binds its
// handle to Name in the step's variable environment:
// `spawn review = Reviewer.run(prompt: "...")`. Call must be an
// `<Agent>.run(...)` expression — the grammar accepts any expression here and
// the interpreter rejects anything else, keeping this node small. The handle
// is joined by a later WaitStmt, or automatically when the step body ends
// (drainAtStepEnd). `spawn` is only meaningful directly inside a step body;
// the interpreter errors if evaluated anywhere else (a tool method, a
// `describe` block).
type SpawnStmt struct {
	Name string `parser:"'spawn' @Ident '='"`
	Call *Expr  `parser:"@@"`
}

// WaitStmt joins one or more spawned handles by name. `wait a, b` waits for
// all of them, failing the step on the first error (and cancelling the
// rest); `wait any a, b` returns as soon as one succeeds; `wait 2 of a, b, c`
// returns once that many have succeeded. Trailing `timeout: <duration>` and
// `on_error: "collect"` options reuse the `key: value` shape of the config
// blocks rather than introducing a braced options block. `any` and `of` are
// effectively reserved in this position — a spawn handle should not be named
// either.
type WaitStmt struct {
	Any    bool       `parser:"'wait' ( @'any'"`
	Quorum string     `parser:"| ( @Number 'of' ) )?"`
	Names  []string   `parser:"@Ident ( ',' @Ident )*"`
	Opts   []*WaitOpt `parser:"@@*"`
}

// WaitOpt is one trailing `timeout: 30s` / `on_error: "collect"` option on a
// WaitStmt. Key is restricted to those two literals so the repetition stops
// cleanly at the next statement.
type WaitOpt struct {
	Key   string `parser:"@( 'timeout' | 'on_error' ) ':'"`
	Value *Expr  `parser:"@@"`
}

// GotoStmt transfers control to another named step in the same declaration,
// abandoning the rest of the current step's body. Target may be any step,
// forward or backward, which is what a "recascade" (jump back to an earlier
// phase after a failed review) or a replan transition needs — the step
// sequence becomes an explicitly branching state machine, not a fixed array
// walk. Only legal inside a `workflow` (not a plain `pipeline`); a step name
// that isn't declared in the same declaration is an error — both enforced
// by internal/lang/lint (checkPipelineGoto), the parser accepts the shape
// in either.
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

// AssignStmt assigns to an lvalue: `x = expr`, `arr[i] = expr`, or the
// compound form `x += expr` — sugar for `x = x + expr` that reuses the
// binary `+` operator's operand rules exactly (two numbers add, two strings
// concatenate, two arrays combine into a fresh slice). Op is "=" or "+=".
type AssignStmt struct {
	Target *Postfix `parser:"@@"`
	Op     string   `parser:"@( '+=' | '=' )"`
	Value  *Expr    `parser:"@@"`
}

// ExprStmt is a bare expression used for its side effects, e.g. a call.
type ExprStmt struct {
	Expr *Expr `parser:"@@"`
}
