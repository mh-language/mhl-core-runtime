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
// Max is the optional `max <N>` header clause — `loop workflow Refine max 3
// { ... }` — shorthand for `repeat { max_iterations: N }`, the iteration
// ceiling LoopRunner enforces. It reads as a bare Number token in the
// declaration header (like a step's `timeout <dur>` clause), so `max` is a
// contextually reserved word in that position only. Empty when the clause is
// absent; runtime.PipelineFromAST parses it and lint rejects a non-positive
// value, its use without the `loop` prefix, and its use alongside an
// explicit `repeat { max_iterations }` (which wins).
//
// Kind is the declaration keyword: "pipeline" (steps run in order, each once
// — `goto` is a lint error) or "workflow" (identical execution model, but
// `goto <step>` is allowed, so the step sequence is an explicitly branching
// state machine). The distinction is purely static — the interpreter and
// runtime treat both the same; internal/lang/lint is what rejects `goto`
// outside a `workflow` (checkPipelineGoto).
//
// Partial marks this declaration as one fragment of a pipeline/workflow
// whose full input/var/route/step set is assembled from several files —
// e.g. one file declares `partial workflow Discovery { entry step Dispatch
// { ... } }` and a sibling, pulled in with a whole-file `import "..."`
// (Import.Items empty — see program.go), declares `partial workflow
// Discovery { step BriefGenerate { ... } }`. lint.mergePartials (mirrored at
// run time by interpreter's resolvePartials, the same way import merging
// already is) collapses every fragment sharing Name+Kind into one Pipeline —
// with one concatenated Body — before runtime.PipelineFromAST or any other
// pipeline-shaped lint check ever runs, so nothing downstream of that merge
// needs to know a pipeline was ever split. A non-partial declaration is
// unaffected: `Partial` false means exactly what it means today, one
// self-contained declaration, and a `Step.Entry` marker is a lint error
// there (see Step.Entry) since the first physically declared step still
// wins.
//
// Param and Returns are the optional typed signature —
// `workflow Review(req: ReviewInput): ReviewOutput { ... }`, the same
// `name(param): Type` shape a tool method uses. Param binds the caller's
// arguments as one object whose fields are the inputs (so it replaces, and
// lint forbids mixing it with, per-line `input x: T` members); Returns
// declares the result type the `output: { ... }` projection must satisfy
// (lint requires an explicit `output:` alongside it). Both nil means the
// untyped, per-line-input form, unchanged.
type Pipeline struct {
	Pos    lexer.Position
	EndPos lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	// Internal (`internal workflow X`) keeps the declaration from being an
	// entry point: `mhl serve` does not publish it and `mhl run` does not
	// pick it. It still runs as a workflow alias's target and through
	// `X.run(...)` in tests.
	Internal bool           `parser:"@'internal'?" digest:"omitzero"`
	Partial  bool           `parser:"@'partial'?"`
	Loop     bool           `parser:"@'loop'?"`
	Kind     string         `parser:"@( 'pipeline' | 'workflow' )"`
	Name     string         `parser:"@Ident"`
	Param    *PipelineParam `parser:"( '(' @@ ')' )?" digest:"omitzero"`
	Returns  *TypeExpr      `parser:"( ':' @@ )?" digest:"omitzero"`
	Max      string         `parser:"( 'max' @Number )?"`
	// Stage (`partial workflow Story stage Implementation { ... }`) puts the
	// whole fragment in one stage — the only way to declare one: a workflow
	// that wants named phases is split into one `partial` fragment per phase.
	// Every step and parallel group the fragment declares belongs to it
	// (parser.expandStages tags them: Step.Stage, ParallelGroup.Stage, which
	// is what the digest sees). Only on a `partial` declaration (lint).
	Stage string            `parser:"( 'stage' @Ident )?" digest:"-"`
	Body  []*PipelineMember `parser:"'{' @@* '}'"`
	// Stages holds the fragment's header stage as parser.expandStages found
	// it (at most one; none on a merged partial — its members carry their
	// stage), kept for its span and member order.
	Stages []*Stage `parser:"" digest:"-"`
}

// Stage is a fragment's header stage (Pipeline.Stage): a named phase of a
// workflow. It is organisational — the steps keep their own names (a `goto`
// still names a step, checkpoints are unchanged) — plus two things: `goto
// Name` enters the stage at its first member, and step events, StepContext
// and a run's status name the stage a step belongs to. Pos..EndPos is the
// whole fragment.
type Stage struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Name   string
	// Members are the names of the stage's steps and parallel groups, in order.
	Members []string
}

// PipelineParam is the single `name: Type` parameter of a typed
// pipeline/workflow signature (see Pipeline.Param).
type PipelineParam struct {
	Pos  lexer.Position
	Name string    `parser:"@Ident ':'"`
	Type *TypeExpr `parser:"@@"`
}

// EntryStepCount counts how many of this declaration's steps (including a
// `parallel` group's branch steps) and routes are marked `entry`. A `partial`
// declaration needs exactly one across every fragment merged into it before
// it's usable — runtime.FindPipeline is where that's actually enforced,
// deliberately not at import-resolution time: a lone fragment file (its
// siblings never pulled in, e.g. when a directory of workflows is scanned
// file-by-file — see execsvc.Load) legitimately has zero, and that's not by
// itself a bug worth failing an unrelated file's `mhl test`/`mhl serve mcp`
// over.
func (p *Pipeline) EntryStepCount() int {
	n := 0
	for _, m := range p.Body {
		if m.Step != nil && m.Step.Entry {
			n++
		}
		if m.Route != nil && m.Route.Entry {
			n++
		}
		if m.Parallel != nil {
			for _, s := range m.Parallel.Steps {
				if s.Entry {
					n++
				}
			}
		}
	}
	return n
}

// EntryRoute is the declaration's `entry route`, or nil — the first one
// when several are (wrongly) marked; lint.checkEntryRoutes reports that.
func (p *Pipeline) EntryRoute() *WorkflowRoute {
	if p == nil {
		return nil
	}
	for _, m := range p.Body {
		if m.Route != nil && m.Route.Entry {
			return m.Route
		}
	}
	return nil
}

// IsWorkflow reports whether this declaration used the `workflow` keyword
// (rather than `pipeline`), which is what permits `goto` in its steps.
func (p *Pipeline) IsWorkflow() bool { return p.Kind == "workflow" }

// PipelineBodyProperty is one `name: <expr>` property allowed directly in a
// pipeline/workflow body — a Property node (bare `Ident ':' expr`), not a
// grammar keyword. PipelineBodyProperties below is the single source of truth
// three packages share, so adding one property is one edit here plus wiring
// its value into runtime.PipelineFromAST:
//
//   - internal/engine/runtime.PipelineFromAST — reads the value
//   - internal/lang/lint.checkPipelineProperties — rejects an unknown name
//   - internal/lsp — offers it in body completion
type PipelineBodyProperty struct {
	Name string
	// Doc is the one-line detail shown in editor completion.
	Doc string
	// LoopOnly marks a property that only takes effect on a `loop`
	// pipeline/workflow (today: repeat) — lint still accepts it on a plain
	// pipeline, but completion only offers it under `loop`.
	LoopOnly bool
	// ParamType names the builtin global type (types.go's `aliases` table —
	// "SessionContext", "StepContext", "FailureContext") this property's
	// single lambda parameter is bound to, for a lifecycle hook property;
	// empty for every other property (these aren't lambdas at all). The
	// runtime never checks a hook lambda's parameter against this — mhl
	// lambdas stay dynamically typed, exactly like before/after on agent —
	// it exists solely so internal/lsp can resolve `session.`/`step.`/
	// `failure.` to real field completion (see internal/lsp/hookcontext.go)
	// without a second, hand-duplicated field list to keep in sync with
	// execsvc.go's actual payload construction.
	ParamType string
}

// PipelineBodyProperties is the allow-list of pipeline/workflow body
// properties. Keep runtime.PipelineFromAST's Prop.Name switch in step with it.
var PipelineBodyProperties = []PipelineBodyProperty{
	{Name: "description", Doc: `"..." — human-readable summary; surfaced as the MCP tool / A2A skill description by "mhl serve"`},
	{Name: "checkpoint", Doc: "{ enabled, strategy, storage, ttl } — optional; per-step checkpointing is on by default, declare enabled: false to opt out"},
	{Name: "spawn", Doc: "{ max_concurrency } — run-wide ceiling on concurrent spawned agent calls"},
	{Name: "context", Doc: "{ source, require } — populate context.vars from a prior run (context.session_id / .started_at / .resumed / .principal need no block)"},
	{Name: "output", Doc: "{ name: expr, ... } — explicit result projection; with it declared, only these keys are returned to a caller (and over MCP / A2A) instead of every var"},
	{Name: "repeat", Doc: "{ stop_when, max_iterations } — the `max <N>` header clause is shorthand for just max_iterations", LoopOnly: true},
	{Name: "session_start", Doc: "(session: SessionContext) -> { ... } — runs once at the very start of the run, before its first step (fires again with session.resumed=true on --resume)", ParamType: "SessionContext"},
	{Name: "session_end", Doc: "(session: SessionContext) -> { ... } — runs once when the run completes normally, via complete(), or via break (session.iterations only on a loop) — not on pause() or on a genuine failure (see stop_failure)", ParamType: "SessionContext"},
	{Name: "step_start", Doc: "(step: StepContext) -> { ... } — runs before every step execution, including each parallel branch and each loop iteration", ParamType: "StepContext"},
	{Name: "step_end", Doc: `(step: StepContext) -> { ... } — runs after every step execution; step.error is the failure message, or null on success/break/pause()/complete()/goto`, ParamType: "StepContext"},
	{Name: "stop_failure", Doc: `(failure: FailureContext) -> { ... } — runs once on a genuine failure; failure.reason is "failed"|"timeout"|"cancelled"|"max_step_visits"|"runtime_error" — never on a soft loop stop like max_iterations`, ParamType: "FailureContext"},
}

// PipelineBodyPropertyNames returns the allow-list as a set, for a membership
// check.
func PipelineBodyPropertyNames() map[string]bool {
	m := make(map[string]bool, len(PipelineBodyProperties))
	for _, p := range PipelineBodyProperties {
		m[p.Name] = true
	}
	return m
}

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
	Pos      lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	EndPos   lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	Input    *PipelineInput `parser:"( 'input' @@"`
	Const    *ConstDecl     `parser:"| @@"`
	Var      *VarDecl       `parser:"| @@"`
	Mem      *MemDecl       `parser:"| @@"`
	Parallel *ParallelGroup `parser:"| @@"`
	Route    *WorkflowRoute `parser:"| @@"`
	Step     *Step          `parser:"| @@"`
	Prop     *Property      `parser:"| @@ )"`
}

// WorkflowRoute is a reusable, statically checked step dispatch table.
//
// Entry (`entry route Choose(model: M) { ... }`) makes it the workflow's
// starting point: the run begins by dispatching on the workflow input
// named like its parameter, before any step. It runs as a stage of its own,
// under the route's name (runtime.PipelineFromAST puts it first;
// interpreter.RunStep evaluates it via execEntryRoute), so it shows up in
// step events and checkpoints like a step. It stays reusable through
// `goto Choose(x)`. lint.checkEntryRoutes requires a `workflow`, at most one
// entry, and an input matching the parameter's name and type.
type WorkflowRoute struct {
	Pos    lexer.Position
	EndPos lexer.Position  `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	Entry  bool            `parser:"@'entry'?" digest:"omitzero"`
	Name   string          `parser:"'route' @Ident"`
	Params []*Param        `parser:"'(' ( @@ ( ',' @@ )* )? ')'"`
	Arms   []*GotoMatchArm `parser:"'{' @@+ '}'"`
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
//
// An optional `merge (branches) { ... }` block, last in the group, decides
// how the branches' writes combine (see ParallelMerge).
//
// Vars, declared before the branches (`var left = ""`), are the group's own
// variables: initialized each time the group starts (runtime.Pipeline.
// GroupVarsInit, interpreter.EvalGroupVars), visible to its branches and its
// merge exactly like workflow vars (copied per branch, merged at the
// barrier), and gone once the group ends — never in a later step, the run's
// result or a checkpoint. A name may not reuse a workflow input/var/const/
// mem (lint.checkParallelMerges).
type ParallelGroup struct {
	Pos     lexer.Position
	EndPos  lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	Name    string         `parser:"'parallel' @Ident"`
	Timeout string         `parser:"( 'timeout' @Duration )?"`
	Vars    []*GroupVar    `parser:"'{' @@*" digest:"omitzero"`
	Steps   []*Step        `parser:"@@+"`
	Merge   *ParallelMerge `parser:"@@? '}'" digest:"omitzero"`
	// Stage is as Step.Stage.
	Stage string `parser:"" digest:"omitzero"`
}

// GroupVar is one `var name = expr` of a parallel group (ParallelGroup.Vars).
type GroupVar struct {
	Pos    lexer.Position
	EndPos lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	Decl   *VarDecl       `parser:"@@"`
}

// ParallelMerge is a parallel group's explicit join: a body that runs once,
// after every branch finished, in the workflow's own scope, with Param bound
// (read-only) to an object holding each branch's variables by step name —
//
//	merge (branches) {
//	    self.reply = [...branches.Left.reply, ...branches.Right.reply]
//	}
//
// Param is optional (`merge { ... }`, Param ""): with group vars, one per
// branch, the merge rarely needs the branches' own views.
//
// Without one, two branches assigning one variable different values is a
// hard error (runtime.Runner's runParallelStage). With one, that variable
// keeps its pre-group value for the merge to read and must be assigned by
// it; every variable the branches agree on is merged as before, so the merge
// sees it already combined. `goto`, `break`, `pause` and `complete` are
// rejected inside it (lint.checkParallelGroups): it is part of the group's
// barrier, not a step of the state machine.
type ParallelMerge struct {
	Pos    lexer.Position
	EndPos lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	Param  string         `parser:"'merge' ( '(' @Ident? ')' )?"`
	Body   []*Statement   `parser:"'{' @@* '}'"`
}

// MergeStepSuffix joins a group's name to the step name its merge runs
// under ("Fan.merge"): not an identifier, so it can never collide with a
// declared step or be a goto target.
const MergeStepSuffix = ".merge"

// MergeStepName is the step name group's merge runs under — what `mhl run`
// prints, step_start/step_end hooks and run events report.
func MergeStepName(group string) string { return group + MergeStepSuffix }

// MergeStep is the group's merge as a step (MergeStepName, the merge's
// body), or nil without one — so everything that walks step bodies covers
// it. A fresh value per call.
func (pg *ParallelGroup) MergeStep() *Step {
	if pg == nil || pg.Merge == nil {
		return nil
	}
	return &Step{Pos: pg.Merge.Pos, EndPos: pg.Merge.EndPos, Name: MergeStepName(pg.Name), Body: pg.Merge.Body}
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

// PipelineInput is a typed pipeline input, e.g. `input issue_id: string`. It
// may carry a default value — `input item_type: string = ""` — mirroring
// Param's `= expr` syntax (program.go). Default is nil when none was
// written, which keeps the input required exactly as before; when present,
// a caller may omit it (runtime.Pipeline.ValidateInputs/InputSchema no
// longer require it) and interpreter.EvalPipelineVars evaluates the
// expression once per run to seed it, the same way a pipeline-level `var`
// initializer is evaluated — a caller-supplied value still always wins
// (execsvc merges coerced inputs on top after init runs). Unlike Param,
// there is no positional-order constraint to enforce: pipeline inputs are
// always named, never positional, so a defaulted input may be declared
// before a required one.
type PipelineInput struct {
	Pos     lexer.Position
	EndPos  lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	Name    string         `parser:"@Ident ':'"`
	Type    *TypeExpr      `parser:"@@"`
	Default *Expr          `parser:"( '=' @@ )?"`
	// Stage names the stage of the fragment the input was declared in
	// (`partial workflow X stage S { input a: T ... }`), set by
	// parser.expandStages; "" for a workflow-level input (and always for an
	// agent's). A stage input is not required when the run starts: the run
	// pauses on entering S until it is supplied (runtime.InputGate).
	Stage string `parser:"" digest:"omitzero"`
	// Doc is the input's documentation: the `///` lines right above it,
	// joined (parser.attachInputDocs). Not part of the definition — editing
	// it never invalidates a checkpoint. "" when it has none.
	Doc string `parser:"" digest:"-"`
}

// Step is a named block of statements. An optional `timeout <duration>`
// header clause (`step Build timeout 3m { ... }`) caps how long the step may
// run: runtime.Runner wraps the step's context with context.WithTimeout, and
// an expiry fails the step the same way an explicit fail() would — a
// checkpoint is written when checkpointing is enabled, so `run/resume`
// re-enters the step with a fresh budget. A step literally named `timeout`
// cannot also carry the clause; the word is reserved in that position, like
// `parallel`/`any`/`of`.
//
// Entry marks this step as the pipeline's starting point — `entry step
// Dispatch { ... }`. It exists only for a `Pipeline.Partial` declaration:
// runner.go's execStage otherwise starts at Steps[0], "the first step
// physically declared", which stops being a stable notion once a pipeline's
// steps are merged in from more than one file. lint.checkPipelineEntry
// rejects `entry` entirely on a non-partial declaration (where
// first-declared-wins is unambiguous already, so a second way to say the
// same thing would just be redundant surface); lint.mergePartialGroup
// reports a partial's merged fragments not carrying exactly one as a
// Finding. runtime.FindPipeline enforces that same "exactly one" as a hard
// error when a partial pipeline is actually resolved to run — see
// EntryStepCount and FindPipeline's doc comment for why that check lives
// there and not earlier, at import-resolution time. runtime.PipelineFromAST
// is what honors a valid `entry`, moving that step's Stage to the front of
// Pipeline.Stages/Steps regardless of source order.
type Step struct {
	Pos     lexer.Position
	EndPos  lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	Entry   bool           `parser:"@'entry'?"`
	Name    string         `parser:"'step' @Ident"`
	Timeout string         `parser:"( 'timeout' @Duration )?"`
	Body    []*Statement   `parser:"'{' @@* '}'"`
	// Stage names the stage of the fragment the step was declared in ("" when
	// the fragment has none, and for a parallel group's branch — its group
	// carries it).
	Stage string `parser:"" digest:"omitzero"`
}

// Statement is a single statement inside a step body. Assignment is tried
// before a bare expression statement so that `x = expr` is not misread as an
// expression; Return is tried before Assign/Expr so `return ...` always
// commits to ReturnStmt rather than being misread as a bare `return`
// identifier expression followed by a second, unrelated statement.
type Statement struct {
	Pos       lexer.Position
	EndPos    lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	Const     *ConstDecl     `parser:"( @@"`
	Var       *VarDecl       `parser:"| @@"`
	Return    *ReturnStmt    `parser:"| @@"`
	Break     *BreakStmt     `parser:"| @@"`
	GotoMatch *GotoMatchStmt `parser:"| @@"`
	Goto      *GotoStmt      `parser:"| @@"`
	Spawn     *SpawnStmt     `parser:"| @@"`
	Wait      *WaitStmt      `parser:"| @@"`
	If        *IfStmt        `parser:"| @@"`
	While     *WhileStmt     `parser:"| @@"`
	ForIn     *ForInStmt     `parser:"| @@"`
	Try       *TryStmt       `parser:"| @@"`
	// Destructure is expanded away by the parser (parser.expandDestructure)
	// before anything else sees the AST; it is always nil on a parsed tree.
	Destructure *DestructureStmt `parser:"| @@" digest:"omitzero"`
	Assign      *AssignStmt      `parser:"| @@"`
	Expr        *ExprStmt        `parser:"| @@ )"`
}

// DestructureStmt binds several fields of one object in a single statement:
//
//	var {data, revisao: note} = review   // declares data, note
//	{tokens_in, tokens_out} = review     // assigns existing names
//	self.{tokens_in, tokens_out} = review // assigns pipeline vars (self.name)
//	base.{tokens_in, tokens_out} = review // the base fragment's (base.name)
//
// Each entry `field` binds the field of the same name; `field: name` binds
// it to a different name. The right-hand side is evaluated once. A missing
// field fails exactly as `review.field` would.
type DestructureStmt struct {
	Pos lexer.Position
	Var bool `parser:"( @'var'"`
	// Root is "self" or "base" for the `self.{...}` / `base.{...}` forms,
	// which assign the fields to that pipeline state; "" for the others.
	Root    string              `parser:"| @( 'self' | 'base' ) '.' )?"`
	Entries []*DestructureEntry `parser:"'{' @@ ( ',' @@ )* ','? '}' '='"`
	Value   *Expr               `parser:"@@"`
}

// DestructureEntry is one `field` or `field: name` of a DestructureStmt.
type DestructureEntry struct {
	Field string `parser:"@Ident"`
	Name  string `parser:"( ':' @Ident )?"`
}

// Target is the name the entry binds: Name when renamed, else Field.
func (e *DestructureEntry) Target() string {
	if e.Name != "" {
		return e.Name
	}
	return e.Field
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
//
// A trailing `for <ident> in <expr>` clause turns it into a fan-out: the
// right-hand `<Agent>.run(...)` call is started once per element of the
// (array) <expr>, with EachVar bound to that element while its argument
// expressions are evaluated — so each spawn can carry a distinct prompt
// (`spawn reviews = R.run(prompt: "check ${item}") for item in items`). Name
// is then bound to an *array* of handles, in element order, which a later
// `wait reviews` / `wait any reviews` / `wait N of reviews` joins as a group
// and which indexes and iterates like any array (`reviews[0].result`,
// `for (var r in reviews) ...`, `reviews.size()`). `for` and `in` are
// effectively reserved in this position. EachVar/Iterable are empty for a
// plain single spawn.
type SpawnStmt struct {
	Name     string `parser:"'spawn' @Ident '='"`
	Call     *Expr  `parser:"@@"`
	EachVar  string `parser:"( 'for' @Ident 'in'"`
	Iterable *Expr  `parser:"@@ )?"`
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
//
// Target may be written bare (`goto Review`) or wrapped in `nameof(...)`
// (`goto nameof(Review)`) — purely a spelling choice, both capture the same
// identifier into Target and are checked identically downstream (nothing
// past the parser ever sees which form was used). `nameof(...)` elsewhere
// in the language (eval.go's evalNameofCall) is a real expression-position
// builtin that validates its argument names a declared top-level thing;
// here it is not evaluated as a call at all — plain grammar sugar so a
// `goto` target reads the same way a router's `select: () -> nameof(X)`
// already does, without goto needing an expression-typed Target (which
// would ripple through every other reader of it — lint's
// checkPipelineGoto, runtime.Runner's goto handling, execsvc's static
// goto listing — since they only ever want the plain step name).
// `nameof(...)`'s own argument-must-be-a-declared-name guarantee is not
// needed here either: checkPipelineGoto already requires Target to name an
// actual step, a strictly narrower and equally-enforced check.
type GotoStmt struct {
	Target string      `parser:"'goto' ( 'nameof' '(' @Ident ')' | @Ident"`
	Called bool        `parser:"( @'('"`
	Args   []*Argument `parser:"( @@ ( ',' @@ )* )? ')' )? )"`
}

// GotoMatchStmt selects a statically named step using match patterns.
type GotoMatchStmt struct {
	Subject *Expr           `parser:"'goto' 'match' @@ '{'"`
	Arms    []*GotoMatchArm `parser:"@@+ '}'"`
}

// GotoMatchArm redirects to Target or fails with a computed reason.
type GotoMatchArm struct {
	Pos      lexer.Position
	EndPos   lexer.Position `parser:"" digest:"-"` // set by the parser; read only by internal/lang/flow
	Wildcard bool           `parser:"( @'_'"`
	Pattern  *Expr          `parser:"| @@ ) '->' ("`
	Fail     *Expr          `parser:"'fail' '(' @@ ')'"`
	Target   string         `parser:"| @Ident )"`
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

// PipelineAlias publishes an existing pipeline/workflow under another name,
// with some of its inputs fixed:
//
//	workflow Delivery = ArtifactFlow with { level: Level.delivery } {
//	    description: "Artifacts for a Feature or a standalone Story."
//	}
//
// The alias runs Target's steps unchanged. It is a separate entry point
// everywhere a pipeline is one — its own MCP tool / A2A skill (with its own
// description), its own `mhl run` target, sessions, checkpoints and `mem`
// state — and its input contract is Target's minus the Bound inputs, which
// a caller can no longer pass. Bound values are literals or enum values
// (`Level.delivery`), checked against the input's declared type. Props may
// only set `description` (inherited from Target when absent).
type PipelineAlias struct {
	Pos lexer.Position
	// Internal hides the alias the way it hides a pipeline (Pipeline.Internal).
	Internal bool        `parser:"@'internal'?" digest:"omitzero"`
	Kind     string      `parser:"@( 'pipeline' | 'workflow' )"`
	Name     string      `parser:"@Ident '='"`
	Target   string      `parser:"@Ident"`
	Bound    *Object     `parser:"( 'with' @@ )?"`
	Props    []*Property `parser:"( '{' @@* '}' )?"`
}

// Description is the alias's own `description:` property, "" when absent.
func (a *PipelineAlias) Description() string {
	for _, p := range a.Props {
		if p.Name == "description" {
			if s, ok := StringValue(p.Value); ok {
				return s
			}
		}
	}
	return ""
}
