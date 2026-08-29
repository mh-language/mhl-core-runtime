package interpreter

import (
	"errors"
	"fmt"
	"io"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// RunStep evaluates a step's statements against a fresh, step-scoped
// variable environment (Env): nothing declared with `var` here survives
// past this one step invocation, not across steps and not across a
// `--resume` — state that needs to survive that is expected to go through
// `memory` instead, exactly as language-design.md's own examples do
// (`session_mem.get("attempt", 0)` to recover after a resume), *or* through
// pipelineEnv, for state that only needs to survive across steps within
// this one run (see EvalPipelineVars). pipelineEnv may be nil — a pipeline
// with no top-level `var` declarations never allocates one. mem is likewise
// nil for a pipeline with no top-level `mem` declarations; unlike
// pipelineEnv it survives across loop iterations and --resume too — see
// MemContext (memvar.go). file is the .mh path being run, used only to
// prefix a failing statement's error with its position (see
// execStatement's positionedError wrap below).
func RunStep(prog *ast.Program, stepName, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore, pipelineEnv Env, mem *MemContext, cctx *ContextView, spawnSem chan struct{}) (err error) {
	step := findStep(prog, stepName)
	if step == nil {
		return fmt.Errorf("pipeline step %q not found", stepName)
	}

	ctx := &evalCtx{prog: prog, store: store, jsonStore: jsonStore, out: out, env: Env{}, pipelineEnv: pipelineEnv, mem: mem, cctx: cctx, file: file}
	// A non-nil spawnSem enables `spawn`/`wait` for this step and confines
	// them to it: drainAtStepEnd joins whatever the step body left running,
	// so no spawned goroutine ever outlives the step (and no handle is live
	// across a checkpoint boundary). On a genuine step failure it cancels
	// those stragglers first rather than blocking on work the step will
	// never consume; a normal finish (including `return`/`break`/`goto`)
	// lets them run to completion.
	if spawnSem != nil {
		ctx.spawns = &spawnRegistry{sem: spawnSem}
		defer func() {
			ctx.spawns.drainAtStepEnd(out, err != nil && !isControlSignal(err))
		}()
	}
	err = execBlock(ctx, step.Body)
	var sig *returnSignal
	if errors.As(err, &sig) {
		// A bare `return` inside a step just exits it early — steps don't
		// produce a value in this runtime, so sig.value (if any) is
		// discarded; this is not an error.
		return nil
	}
	return err
}

// findStep returns the pipeline step named stepName, whether it is a plain
// top-level step or a branch step of a `parallel` group (runtime.Runner
// drives a group's branches by name through RunStep exactly like any other
// step). Returns nil when no pipeline declares such a step.
func findStep(prog *ast.Program, stepName string) *ast.Step {
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		for _, member := range decl.Pipeline.Body {
			if member.Step != nil && member.Step.Name == stepName {
				return member.Step
			}
			if member.Parallel != nil {
				for _, s := range member.Parallel.Steps {
					if s.Name == stepName {
						return s
					}
				}
			}
		}
	}
	return nil
}

// EvalPipelineVars evaluates pipelineName's top-level `var` declarations
// once, in declaration order (each one's expression can reference an
// earlier one, same as sequential `var`s in a step body), into a fresh Env.
// The result is meant to be threaded into every RunStep call of one
// runtime.Runner.Run() execution — see runtime.InitFunc — so a plain
// assignment in one step is visible to the next; it resets to these
// initial values again the next time Run() executes the pipeline (e.g. the
// next loop iteration), since that calls this fresh rather than reusing
// the prior run's map. A pipeline with no `var` declarations returns an
// empty, non-nil Env — not an error. cctx (may be nil) is visible to a
// var's initializer expression, so `var id = context.session_id` works.
func EvalPipelineVars(prog *ast.Program, pipelineName, file string, out io.Writer, store *memory.KVStore, jsonStore *memory.JSONStore, cctx *ContextView) (Env, error) {
	var pipeline *ast.Pipeline
	for _, decl := range prog.Decls {
		if decl.Pipeline != nil && decl.Pipeline.Name == pipelineName {
			pipeline = decl.Pipeline
			break
		}
	}
	if pipeline == nil {
		return nil, fmt.Errorf("pipeline %q not found", pipelineName)
	}

	env := Env{}
	ctx := &evalCtx{prog: prog, store: store, jsonStore: jsonStore, out: out, env: env, cctx: cctx, file: file}
	for _, member := range pipeline.Body {
		if member.Var == nil {
			continue
		}
		v, err := evalExpr(ctx, member.Var.Value)
		if err != nil {
			return nil, err
		}
		env[member.Var.Name] = v
	}
	return env, nil
}

// returnSignal is not a real error: it's how a `return` statement's
// control-flow unwinds up through execBlock/execIf/execWhile/execTry
// (which already just propagate any non-nil error immediately) to whoever
// is waiting for it — evalToolCall for a tool method's block body, or
// RunStep for a bare `return` inside a step. It's never shown to a user;
// see execTry for the one place that must NOT treat it like a genuine
// error (a return must skip catch but still run finally).
type returnSignal struct{ value any }

func (r *returnSignal) Error() string { return "return" }

// breakSignal and gotoSignal are the same kind of not-a-real-error control
// flow returnSignal already is, for the two statements pipeline.go added
// alongside it. Both are meant to reach all the way out of RunStep (unlike
// returnSignal, which RunStep itself swallows) — cli.go's exec closure is
// what inspects them, via IsBreak/IsGoto below, and translates each into the
// runtime package's own BreakSignal/GotoSignal for Runner.Run to act on;
// interpreter and runtime stay independent of each other's types the same
// way they already are everywhere else (see internal/engine/runtime's
// package doc).
type breakSignal struct{ reason any }

func (b *breakSignal) Error() string { return "break" }

type gotoSignal struct{ target string }

func (g *gotoSignal) Error() string { return "goto " + g.target }

// isControlSignal reports whether err is one of the three not-a-real-error
// control-flow signals (return/break/goto) rather than a genuine failure —
// the shared check execStatement (skip the positionedError wrap) and
// execTry (skip Catch, still run Finally) both need.
func isControlSignal(err error) bool {
	var retSig *returnSignal
	var brkSig *breakSignal
	var gtSig *gotoSignal
	return errors.As(err, &retSig) || errors.As(err, &brkSig) || errors.As(err, &gtSig)
}

// IsBreak reports whether err (as returned by RunStep) came from a `break`
// statement, and the value its optional reason expression evaluated to (nil
// when break carried none).
func IsBreak(err error) (reason any, ok bool) {
	var sig *breakSignal
	if errors.As(err, &sig) {
		return sig.reason, true
	}
	return nil, false
}

// IsGoto reports whether err (as returned by RunStep) came from a `goto
// Target` statement, and that target step's name.
func IsGoto(err error) (target string, ok bool) {
	var sig *gotoSignal
	if errors.As(err, &sig) {
		return sig.target, true
	}
	return "", false
}

// maxLoopIterations caps how many times a `while` loop's body may run, so a
// pipeline bug (a condition that never turns false) fails with a clear error
// instead of hanging `mhl run` forever. Mirrors the existing maxValueDepth
// safety-cap convention (eval.go).
const maxLoopIterations = 10_000

// execBlock runs a list of statements in order against ctx's environment,
// stopping at the first error. All six Statement shapes the grammar allows
// (internal/lang/ast/pipeline.go) are handled here.
func execBlock(ctx *evalCtx, statements []*ast.Statement) error {
	for _, statement := range statements {
		if err := execStatement(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// positionedError decorates a runtime error with the file:line:column of
// the statement that raised it, mirroring the position lint already
// reports for static errors (internal/lang/lint.Finding). execStatement is
// the single wrap point: nested statements (an if/while/try body, a
// closure's or tool method's block) wrap first as their own error
// propagates outward, and the errors.As guard below stops each enclosing
// execStatement from re-wrapping (and overwriting) that innermost, most
// relevant position.
type positionedError struct {
	file string
	pos  lexer.Position
	err  error
}

func (e *positionedError) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.file, e.pos.Line, e.pos.Column, e.err)
}

func (e *positionedError) Unwrap() error { return e.err }

// errorMessage returns err's message with any positionedError prefix
// stripped, for binding to `catch (e)`. A caught error is meant to hold
// the same plain diagnostic text pipeline logic matches against (e.g. `if
// (e == "not found")`), not the file:line:column prefix that's only useful
// once an error becomes fatal and reaches RunStep's caller uncaught.
func errorMessage(err error) string {
	var posErr *positionedError
	if errors.As(err, &posErr) {
		return posErr.err.Error()
	}
	return err.Error()
}

func execStatement(ctx *evalCtx, statement *ast.Statement) error {
	err := execStatementBody(ctx, statement)
	if err == nil {
		return nil
	}
	if isControlSignal(err) {
		return err
	}
	var posErr *positionedError
	if errors.As(err, &posErr) {
		return err
	}
	return &positionedError{file: ctx.file, pos: statement.Pos, err: err}
}

func execStatementBody(ctx *evalCtx, statement *ast.Statement) error {
	switch {
	case statement.Var != nil:
		v, err := evalExpr(ctx, statement.Var.Value)
		if err != nil {
			return err
		}
		ctx.env[statement.Var.Name] = v
		return nil
	case statement.Return != nil:
		var v any
		if statement.Return.Value != nil {
			var err error
			v, err = evalExpr(ctx, statement.Return.Value)
			if err != nil {
				return err
			}
		}
		return &returnSignal{value: v}
	case statement.Break != nil:
		var reason any
		if statement.Break.Reason != nil {
			var err error
			reason, err = evalExpr(ctx, statement.Break.Reason)
			if err != nil {
				return err
			}
		}
		return &breakSignal{reason: reason}
	case statement.Goto != nil:
		return &gotoSignal{target: statement.Goto.Target}
	case statement.Spawn != nil:
		return execSpawn(ctx, statement.Spawn)
	case statement.Wait != nil:
		return execWait(ctx, statement.Wait)
	case statement.Assign != nil:
		return execAssign(ctx, statement.Assign)
	case statement.If != nil:
		return execIf(ctx, statement.If)
	case statement.While != nil:
		return execWhile(ctx, statement.While)
	case statement.ForIn != nil:
		return execForIn(ctx, statement.ForIn)
	case statement.Try != nil:
		return execTry(ctx, statement.Try)
	case statement.Expr != nil:
		return execExprStatement(ctx, statement.Expr.Expr)
	}
	return nil
}

// execExprStatement evaluates a bare expression statement for its side
// effect, discarding the value. log(...) is recognized inside the general
// expression evaluator itself now (evalPostfix/evalLogCall in eval.go), not
// specially here — that's what lets it appear anywhere an expression can,
// including a `tool` method body, not just as a top-level statement.
//
// When ctx.assertions is set (running a `describe` block's body, see
// test.go), a bare call to a builtin assertion function is recognized here
// first and recorded rather than evaluated as an ordinary expression — this
// is what lets an assertion appear nested inside if/while/for-in without
// any dedicated grammar or a parallel statement-execution path: it's the
// exact same execBlock/execIf/execWhile/execForIn (below) a pipeline step
// uses, just with this one interception point.
func execExprStatement(ctx *evalCtx, expr *ast.Expr) error {
	if ctx.assertions != nil {
		if name, call, ok := assertionCall(expr); ok {
			result, err := evalAssertionCall(ctx, name, call)
			if err != nil {
				return err
			}
			*ctx.assertions = append(*ctx.assertions, result)
			return nil
		}
	}
	_, err := evalExpr(ctx, expr)
	return err
}

// execAssign handles `x = expr` and `x[i] = expr` (chained: `matrix[i][j] =
// expr`, `config[section][key] = expr`) — a bare variable, or a variable
// followed by one or more bracket-index trailers, each an integer index
// into an array or a dynamic (runtime-computed) string key into an object.
// Any Member or Call trailer anywhere in the target — e.g. `obj.field =
// expr` — fails closed with a clear error rather than silently no-oping,
// since this interpreter has no mutable-structure semantics beyond
// bracket-index writes (a Member trailer only ever reads a literal,
// parse-time-known field name; dynamic writes go through `obj[key] = v`
// instead). Assigning to a name never declared with `var` is also an
// error: it never implicitly declares, consistent with the rest of the
// interpreter's fail-closed philosophy.
//
// An index write mutates the target array/object in place (indexWrite,
// eval.go) rather than rebuilding and reassigning ctx.env — since a Go
// slice or map already shares its backing storage with every alias of the
// same value (`var b = a` copies the slice/map header, not its contents,
// the same as reading `a` back after `b[0] = ...` or `b["k"] = ...`
// already reflects it today), this is what makes `matrix[i][j] = v` or
// `config[section][key] = v` mutate the outer array/object/env entry it
// was read from at all, with no reassignment step needed at each level of
// the chain.
//
// A name declared with the step's own `var` always wins over a
// same-named pipeline variable (env is checked before pipelineEnv,
// exactly like the read side in evalPrimary) — this is what a step's
// `var` shadowing a pipeline var would mean, though in practice a step
// author has no reason to pick the same name on purpose.
func execAssign(ctx *evalCtx, assign *ast.AssignStmt) error {
	name, ok := assignTargetBase(assign.Target)
	if !ok {
		return fmt.Errorf("assignment target must be a plain variable or an array index, not a nested field")
	}
	target := ctx.env
	if _, declared := target[name]; !declared {
		if _, declared = ctx.pipelineEnv[name]; !declared {
			if isMemVar(ctx, name) {
				return execMemAssign(ctx, name, assign)
			}
			if isContextRef(ctx, name) {
				return fmt.Errorf("cannot assign to %q: the pipeline's context is read-only", name)
			}
			return fmt.Errorf("undefined variable %q", name)
		}
		target = ctx.pipelineEnv
	}
	v, err := evalExpr(ctx, assign.Value)
	if err != nil {
		return err
	}
	ops := assign.Target.Ops
	if len(ops) == 0 {
		if assign.Op == "+=" {
			if v, err = addValues(target[name], v); err != nil {
				return err
			}
		}
		target[name] = v
		return nil
	}
	if assign.Op == "+=" {
		cur, err := applyTrailers(ctx, target[name], ops, 0)
		if err != nil {
			return err
		}
		if v, err = addValues(cur, v); err != nil {
			return err
		}
	}
	container, err := applyTrailers(ctx, target[name], ops[:len(ops)-1], 0)
	if err != nil {
		return err
	}
	return indexWrite(ctx, container, ops[len(ops)-1].Index, v, 0)
}

// assignTargetBase returns the target's base variable name when it has the
// shape execAssign accepts: a bare identifier, or an identifier followed
// only by bracket-index trailers (`arr[i]`, `matrix[i][j]`, `config[key]`)
// — any Member or Call trailer in the chain makes it not an assignable
// target.
func assignTargetBase(p *ast.Postfix) (string, bool) {
	if p == nil || p.Primary == nil || p.Primary.Ident == "" {
		return "", false
	}
	for _, op := range p.Ops {
		if op.Index == nil {
			return "", false
		}
	}
	return p.Primary.Ident, true
}

func execIf(ctx *evalCtx, stmt *ast.IfStmt) error {
	v, err := evalExpr(ctx, stmt.Cond)
	if err != nil {
		return err
	}
	b, ok := v.(bool)
	if !ok {
		return fmt.Errorf("if condition must be a bool, got %s", typeName(v))
	}
	if b {
		return execBlock(ctx, stmt.Then)
	}
	return execBlock(ctx, stmt.Else)
}

func execWhile(ctx *evalCtx, stmt *ast.WhileStmt) error {
	for i := 0; ; i++ {
		if i >= maxLoopIterations {
			return fmt.Errorf("while loop exceeded the maximum of %d iterations", maxLoopIterations)
		}
		v, err := evalExpr(ctx, stmt.Cond)
		if err != nil {
			return err
		}
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("while condition must be a bool, got %s", typeName(v))
		}
		if !b {
			return nil
		}
		if err := execBlock(ctx, stmt.Body); err != nil {
			return err
		}
	}
}

// execForIn evaluates Iterable (must be an array) and runs Body once per
// element, binding it to VarName each time — the same maxLoopIterations
// cap execWhile uses guards against a pathologically large array (e.g.
// loaded from disk via a json memory), even though a for-in loop can never
// truly run forever the way a mistyped while condition can.
func execForIn(ctx *evalCtx, stmt *ast.ForInStmt) error {
	v, err := evalExpr(ctx, stmt.Iterable)
	if err != nil {
		return err
	}
	items, ok := v.([]any)
	if !ok {
		return fmt.Errorf("for-in requires an array, got %s", typeName(v))
	}
	if len(items) > maxLoopIterations {
		return fmt.Errorf("for-in exceeded the maximum of %d iterations", maxLoopIterations)
	}
	for _, item := range items {
		ctx.env[stmt.VarName] = item
		if err := execBlock(ctx, stmt.Body); err != nil {
			return err
		}
	}
	return nil
}

// execTry runs Body; on error it binds ErrName (when given) to the error's
// message and runs Catch. Finally always runs afterward — on success, on a
// caught error, or on an error raised inside Catch itself — and if Finally
// itself errors, that error is what propagates, overriding whatever came
// before it.
//
// A `return`, `break` or `goto` inside Body (or Catch) is not a real error —
// it's control flow unwinding past the try — so it skips Catch entirely
// (nothing to "catch") but Finally still runs, same as
// return-inside-try-finally in any language with the construct.
func execTry(ctx *evalCtx, stmt *ast.TryStmt) error {
	result := execBlock(ctx, stmt.Body)
	if result != nil && !isControlSignal(result) {
		if stmt.ErrName != "" {
			ctx.env[stmt.ErrName] = errorMessage(result)
		}
		result = execBlock(ctx, stmt.Catch)
	}
	if len(stmt.Finally) > 0 {
		if err := execBlock(ctx, stmt.Finally); err != nil {
			return err
		}
	}
	return result
}
