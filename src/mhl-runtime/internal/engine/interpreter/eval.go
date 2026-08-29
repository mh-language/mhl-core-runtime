package interpreter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// Env is a step's variable environment: every `var`/`assign` in a step body
// reads and writes the same Env, for the lifetime of that one step
// execution. It never survives past the step that created it — not across
// steps, and not across a `--resume` — matching how the rest of the runtime
// already treats cross-step state (that's what `memory` is for).
type Env map[string]any

// evalCtx bundles everything expression evaluation needs beyond the
// expression itself: the program (for agent/memory/prompt lookups), the
// live stores those declarations back onto, where agent responses and
// log(...) output get traced, and the current step's variable environment.
type evalCtx struct {
	prog      *ast.Program
	store     *memory.KVStore
	jsonStore *memory.JSONStore
	out       io.Writer
	env       Env
	// pipelineEnv holds a pipeline's top-level `var` declarations
	// (PipelineMember.Var, ast/pipeline.go) — nil outside of RunStep's own
	// step execution (a tool method call, `describe` block, or `stop_when`
	// evaluation never sees it, matching how they already don't see the
	// calling step's env either). Unlike env, it is NOT recreated per step:
	// the same map is threaded through every step of one Runner.Run()
	// execution (one loop iteration, or the whole run when there's no
	// loop), so a plain assignment in one step is visible to the next. A
	// bare identifier read (evalPrimary) and a plain assignment
	// (execAssign) both fall back to pipelineEnv only when the name isn't
	// in the step-local env — env always wins on a name collision, and
	// `var` always writes to env, never to pipelineEnv, so a step can never
	// accidentally redeclare a pipeline variable out from under later
	// steps.
	pipelineEnv Env
	// mem backs a pipeline's `mem` declarations (PipelineMember.Mem) — nil
	// wherever the pipeline declares none. Checked as the third and last
	// fallback tier by evalPrimary's Ident case and execAssign, after env
	// and pipelineEnv, and set (unlike pipelineEnv) in EvalCondition too:
	// unlike a pipeline `var`, a `mem` var is exactly what `stop_when` is
	// meant to be able to read — see MemContext's doc comment (memvar.go).
	mem *MemContext
	// cctx backs a pipeline's `context:` element (PipelineMember.Prop named
	// "context") — nil wherever the pipeline declares none. Checked as the
	// fourth and last fallback tier by evalPrimary's Ident case, after env,
	// pipelineEnv and mem, and set (like mem) in EvalCondition and
	// EvalPipelineVars too. It is read-only: execAssign refuses to assign to
	// `context`. See ContextView (contextview.go).
	cctx *ContextView
	// file is the entry .mh file RunStep was called with, used only to
	// prefix a runtime error with its statement's position (see
	// execStatement's positionedError wrap in exec.go). A declaration
	// pulled in via `import`/`use` (internal/engine/interpreter/imports.go
	// merges it straight into prog.Decls with no origin tracking) still
	// reports this entry file, not its own — the same coarse-grained
	// approximation lint's own per-file scan already makes for imports.
	file string
	// assertions is non-nil only while executing a `describe` block's body
	// (see RunTests, test.go); when set, execExprStatement (exec.go)
	// recognizes a bare call to a builtin assertion function (are_equal,
	// is_true, ...) and records its outcome here instead of evaluating it
	// as an ordinary expression — which would otherwise fail as "undefined
	// variable", since none of these names are a declared tool, agent, or
	// var. A normal pipeline step run (RunStep) leaves this nil, so
	// assertion names stay genuinely undefined there, unchanged.
	assertions *[]AssertionResult
	// selfTool is the `tool` declaration whose method body is currently
	// executing — non-nil only inside evalToolCall's childCtx (tool.go) and
	// whatever closures that method body creates (invokeClosureWithValues
	// propagates it the same way it propagates pipelineEnv/file). It's what
	// `self.method(...)` resolves against in evalPostfix below, so a tool
	// method can call a sibling method on its own tool without repeating the
	// tool's declared name. Nil everywhere else (a pipeline step, a
	// `describe` block, a stop_when condition), where `self` has no meaning.
	selfTool *ast.Tool
	// goctx is the Go context governing blocking work started from this
	// evaluation — today only an agent subprocess/HTTP call (see
	// runAgentAttempt in agent.go). It is nil on the many evalCtx literals
	// built outside a pipeline run (tool method childCtx, describe blocks,
	// EvalCondition) and on the nil *evalCtx agent_test.go passes straight
	// into runAgentAttempt; goctxOf normalizes that to context.Background().
	// A spawned agent goroutine (spawn.go) swaps in a cancellable child so a
	// `wait ... timeout:` or a fail-fast sibling cancellation can abort the
	// underlying subprocess.
	goctx context.Context
	// spawns is the registry of background agent handles created by `spawn`
	// statements in the currently executing step — non-nil only inside
	// RunStep's own step body (never a tool method, describe block, or
	// stop_when condition), which is what makes `spawn` a step-only
	// statement. drainAtStepEnd joins whatever is left in it when the step
	// returns.
	spawns *spawnRegistry
	// inSpawn is true while evaluating inside a spawned agent goroutine. It
	// blocks a nested `spawn` (an agent is an opaque CLI process; there is
	// no channel to drive one from) and redirects an agent's `log:` output
	// to the handle buffer so concurrent spawns of the same agent don't
	// interleave writes to one file.
	inSpawn bool
}

// goctxOf returns the Go context governing blocking work for this evaluation,
// falling back to context.Background() for the many evalCtx literals (and the
// nil *evalCtx from agent_test.go) that never set one.
func goctxOf(ctx *evalCtx) context.Context {
	if ctx == nil || ctx.goctx == nil {
		return context.Background()
	}
	return ctx.goctx
}

// maxValueDepth caps how deeply nested an array/object literal may be, so a
// malformed or pathological .mh file can't blow the stack during
// evaluation. It only counts array/object literal nesting, not parens or
// operator nesting.
const maxValueDepth = 10

// evalExpr evaluates expr to a runtime value: a string, float64, bool,
// []any, or map[string]any. Unlike the rest of the "shallow" evaluator this
// replaces, it resolves identifiers against env, executes memory/agent
// calls for their real return value (not just as a side effect), applies
// arithmetic/comparison/logical operators, and interpolates "${...}" spans
// inside string literals.
func evalExpr(ctx *evalCtx, expr *ast.Expr) (any, error) {
	return evalExprAt(ctx, expr, 0)
}

func evalExprAt(ctx *evalCtx, expr *ast.Expr, depth int) (any, error) {
	v, err := evalOr(ctx, expr.Or, depth)
	if err != nil {
		return nil, err
	}
	// `??` short-circuits: the right-hand side is evaluated only when the
	// value so far is null. Unlike `||`/`&&` neither side must be a bool —
	// `a ?? b` is "a unless it's null, otherwise b".
	for _, op := range expr.Tail {
		if v != nil {
			break
		}
		v, err = evalOr(ctx, op.Rhs, depth)
		if err != nil {
			return nil, err
		}
	}
	return v, nil
}

func evalOr(ctx *evalCtx, e *ast.OrExpr, depth int) (any, error) {
	v, err := evalAnd(ctx, e.Head, depth)
	if err != nil {
		return nil, err
	}
	for _, op := range e.Tail {
		lb, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("'||' requires bool operands, got %s", typeName(v))
		}
		if lb {
			v = true
			continue
		}
		rv, err := evalAnd(ctx, op.Rhs, depth)
		if err != nil {
			return nil, err
		}
		rb, ok := rv.(bool)
		if !ok {
			return nil, fmt.Errorf("'||' requires bool operands, got %s", typeName(rv))
		}
		v = rb
	}
	return v, nil
}

func evalAnd(ctx *evalCtx, e *ast.AndExpr, depth int) (any, error) {
	v, err := evalEq(ctx, e.Head, depth)
	if err != nil {
		return nil, err
	}
	for _, op := range e.Tail {
		lb, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("'&&' requires bool operands, got %s", typeName(v))
		}
		if !lb {
			v = false
			continue
		}
		rv, err := evalEq(ctx, op.Rhs, depth)
		if err != nil {
			return nil, err
		}
		rb, ok := rv.(bool)
		if !ok {
			return nil, fmt.Errorf("'&&' requires bool operands, got %s", typeName(rv))
		}
		v = rb
	}
	return v, nil
}

func evalEq(ctx *evalCtx, e *ast.EqExpr, depth int) (any, error) {
	v, err := evalCmp(ctx, e.Head, depth)
	if err != nil {
		return nil, err
	}
	for _, op := range e.Tail {
		rv, err := evalCmp(ctx, op.Rhs, depth)
		if err != nil {
			return nil, err
		}
		eq := reflect.DeepEqual(v, rv)
		if op.Op == "!=" {
			eq = !eq
		}
		v = eq
	}
	return v, nil
}

func evalCmp(ctx *evalCtx, e *ast.CmpExpr, depth int) (any, error) {
	v, err := evalAdd(ctx, e.Head, depth)
	if err != nil {
		return nil, err
	}
	for _, op := range e.Tail {
		rv, err := evalAdd(ctx, op.Rhs, depth)
		if err != nil {
			return nil, err
		}
		lf, ok1 := v.(float64)
		rf, ok2 := rv.(float64)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("%q requires number operands, got %s and %s", op.Op, typeName(v), typeName(rv))
		}
		switch op.Op {
		case "<":
			v = lf < rf
		case "<=":
			v = lf <= rf
		case ">":
			v = lf > rf
		case ">=":
			v = lf >= rf
		}
	}
	return v, nil
}

func evalAdd(ctx *evalCtx, e *ast.AddExpr, depth int) (any, error) {
	v, err := evalMul(ctx, e.Head, depth)
	if err != nil {
		return nil, err
	}
	for _, op := range e.Tail {
		rv, err := evalMul(ctx, op.Rhs, depth)
		if err != nil {
			return nil, err
		}
		switch op.Op {
		case "+":
			v, err = addValues(v, rv)
			if err != nil {
				return nil, err
			}
		case "-":
			lf, ok1 := v.(float64)
			rf, ok2 := rv.(float64)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("'-' requires number operands, got %s and %s", typeName(v), typeName(rv))
			}
			v = lf - rf
		}
	}
	return v, nil
}

// addValues is the core of the binary `+` operator, shared with the `+=`
// compound assignment (execAssign): two strings concatenate, two arrays
// combine into a fresh slice (neither operand mutated, matching the rest of
// the language's copy-on-combine value semantics), two numbers add. Any
// other pairing is an error.
func addValues(l, r any) (any, error) {
	if ls, ok := l.(string); ok {
		rs, ok := r.(string)
		if !ok {
			return nil, fmt.Errorf("'+' requires both operands to be strings when the left operand is a string, got %s", typeName(r))
		}
		return ls + rs, nil
	}
	if la, ok := l.([]any); ok {
		ra, ok := r.([]any)
		if !ok {
			return nil, fmt.Errorf("'+' requires both operands to be arrays when the left operand is an array, got %s", typeName(r))
		}
		combined := make([]any, 0, len(la)+len(ra))
		combined = append(combined, la...)
		combined = append(combined, ra...)
		return combined, nil
	}
	lf, ok1 := l.(float64)
	rf, ok2 := r.(float64)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("'+' requires two numbers, two strings, or two arrays, got %s and %s", typeName(l), typeName(r))
	}
	return lf + rf, nil
}

func evalMul(ctx *evalCtx, e *ast.MulExpr, depth int) (any, error) {
	v, err := evalUnary(ctx, e.Head, depth)
	if err != nil {
		return nil, err
	}
	for _, op := range e.Tail {
		rv, err := evalUnary(ctx, op.Rhs, depth)
		if err != nil {
			return nil, err
		}
		lf, ok1 := v.(float64)
		rf, ok2 := rv.(float64)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("%q requires number operands, got %s and %s", op.Op, typeName(v), typeName(rv))
		}
		switch op.Op {
		case "*":
			v = lf * rf
		case "/":
			if rf == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			v = lf / rf
		case "%":
			if rf == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			v = math.Mod(lf, rf)
		}
	}
	return v, nil
}

func evalUnary(ctx *evalCtx, u *ast.Unary, depth int) (any, error) {
	v, err := evalPostfix(ctx, u.Operand, depth)
	if err != nil {
		return nil, err
	}
	switch u.Op {
	case "!":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("'!' requires a bool operand, got %s", typeName(v))
		}
		return !b, nil
	case "-":
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("unary '-' requires a number operand, got %s", typeName(v))
		}
		return -f, nil
	default:
		return v, nil
	}
}

// evalIfExpr evaluates the ternary-like if-expression form (IfExpr,
// expr.go): Cond must be a bool, and the chosen branch (Then or Else) is
// evaluated and returned as the expression's value. Unlike execIf
// (exec.go) — which runs a *list* of statements for their side effects and
// produces no value — exactly one of Then/Else always runs here, since an
// expression must evaluate to something.
func evalIfExpr(ctx *evalCtx, e *ast.IfExpr, depth int) (any, error) {
	v, err := evalExprAt(ctx, e.Cond, depth)
	if err != nil {
		return nil, err
	}
	b, ok := v.(bool)
	if !ok {
		return nil, fmt.Errorf("if expression condition must be a bool, got %s", typeName(v))
	}
	if b {
		return evalExprAt(ctx, e.Then, depth)
	}
	return evalExprAt(ctx, e.Else, depth)
}

// evalPostfix resolves a Postfix chain. Reserved namespace calls
// (`cmd.exec(...)`, etc. — see nativeNamespaces), `name.run(...)` on a
// declared agent, `self.<method>(...)` on the tool currently executing
// (ctx.selfTool), `name.<method>(...)` on a declared memory, and
// `name.<method>(...)` on a declared tool, and `name.call(...)` on a
// declared mcp_server are all recognized first (and keep their existing,
// specific "not found" errors) — anything else falls
// through to a generic identifier/literal lookup followed by plain member
// access, so a variable holding an object (e.g. from memory.get()) can
// have its fields read with `.field`.
//
// A bare `Name(...)` call — no member trailer at all — whose Name matches a
// declared `prompt Name(...) { "..." }` template is also recognized here,
// rendering it into a plain string via renderPromptCallDynamic
// (prompt_ops.go). This is what lets a prompt reference be used anywhere an
// expression can (assigned to a `var`, passed as an ordinary argument to a
// tool method, concatenated, ...) and not just inside the one special
// `prompt:` position `<Agent>.run(prompt: Name(...))` already recognized
// via resolvePromptArgument — the two share this same rendering function,
// so `Name(...)`'s own arguments are ordinary expressions in both places
// (a variable, `current.title`, another prompt call, ...), not just a
// literal string token. When Name doesn't match any declared prompt, this
// falls through to the generic path below exactly like the `member ==
// "run"` agent case just above does for an unknown agent name — so calling
// an actual closure-holding variable (`predicate(item)`) is unaffected.
//
// Memory vs. tool is decided by what `name` actually is, not by whether
// `member` happens to spell one of the four memory op names — a `tool`
// method is free to be named get/set/append/remove (as e.g. a caching
// tool's own get/set naturally would), and such a name must still resolve
// to that tool rather than being shadowed into a "memory not found" error
// for a memory that was never declared.
func evalPostfix(ctx *evalCtx, p *ast.Postfix, depth int) (any, error) {
	if p.Primary.Ident == "log" && len(p.Ops) == 1 && p.Ops[0].Call != nil {
		return evalLogCall(ctx, p.Ops[0].Call.Args, depth)
	}
	if p.Primary.Ident == "fail" && len(p.Ops) == 1 && p.Ops[0].Call != nil {
		return evalFailCall(ctx, p.Ops[0].Call.Args, depth)
	}
	if p.Primary.Ident == "env" && len(p.Ops) == 1 && p.Ops[0].Call != nil {
		return evalEnvCall(ctx, p.Ops[0].Call.Args, depth)
	}
	if p.Primary.Ident != "" && len(p.Ops) == 1 && p.Ops[0].Call != nil {
		if v, handled, err := evalTypeBuiltinCall(ctx, p.Primary.Ident, p.Ops[0].Call.Args, depth); handled {
			return v, err
		}
	}
	if p.Primary.Ident != "" && len(p.Ops) >= 1 && p.Ops[0].Call != nil {
		if _, ok := findPrompt(ctx.prog, p.Primary.Ident); ok {
			rendered, err := renderPromptCallDynamic(ctx, p.Primary.Ident, p.Ops[0].Call, depth)
			if err != nil {
				return nil, err
			}
			return applyTrailers(ctx, rendered, p.Ops[1:], depth)
		}
	}
	if p.Primary.Ident != "" && !isBoundVar(ctx, p.Primary.Ident) && len(p.Ops) >= 2 && p.Ops[0].Member != "" && !p.Ops[0].Optional && p.Ops[1].Call != nil {
		name := p.Primary.Ident
		member := p.Ops[0].Member
		call := p.Ops[1].Call
		switch {
		case nativeNamespaces[name]:
			v, err := nativeOpCall(ctx, name, member, call, depth)
			if err != nil {
				return nil, err
			}
			return applyTrailers(ctx, v, p.Ops[2:], depth)
		case member == "run":
			if agent, ok := findAgent(ctx.prog, name); ok {
				v, err := runAgent(ctx, name, agent, call, depth)
				if err != nil {
					return nil, err
				}
				return applyTrailers(ctx, v, p.Ops[2:], depth)
			}
		case name == "self":
			if ctx.selfTool == nil {
				return nil, fmt.Errorf("self is only valid inside a tool method")
			}
			v, err := evalToolCall(ctx, ctx.selfTool, member, call, depth)
			if err != nil {
				return nil, fmt.Errorf("self.%s: %w", member, err)
			}
			return applyTrailers(ctx, v, p.Ops[2:], depth)
		case isMemVar(ctx, name):
			if member != "reset" {
				return nil, fmt.Errorf("mem %q has no method %q", name, member)
			}
			if len(call.Args) != 0 {
				return nil, fmt.Errorf("%s.reset: takes no arguments", name)
			}
			if err := resetMemVar(ctx, name); err != nil {
				return nil, err
			}
			return applyTrailers(ctx, nil, p.Ops[2:], depth)
		default:
			if mem, ok := findMemory(ctx.prog, name); ok {
				if !isMemoryMethod(member) {
					return nil, fmt.Errorf("memory %q has no method %q", name, member)
				}
				v, err := executeMemoryOp(ctx, mem, member, call, depth)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %w", name, member, err)
				}
				return applyTrailers(ctx, v, p.Ops[2:], depth)
			}
			if tool, ok := findTool(ctx.prog, name); ok {
				v, err := evalToolCall(ctx, tool, member, call, depth)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %w", name, member, err)
				}
				return applyTrailers(ctx, v, p.Ops[2:], depth)
			}
			if server, ok := findMCPServer(ctx.prog, name); ok {
				v, err := evalMCPServerCall(ctx, server, member, call, depth)
				if err != nil {
					return nil, err
				}
				return applyTrailers(ctx, v, p.Ops[2:], depth)
			}
			if agent, ok := findA2AAgent(ctx.prog, name); ok {
				v, err := evalA2AAgentCall(ctx, agent, member, call, depth)
				if err != nil {
					return nil, err
				}
				return applyTrailers(ctx, v, p.Ops[2:], depth)
			}
			if isMemoryMethod(member) {
				return nil, fmt.Errorf("memory %q not found", name)
			}
		}
	}
	base, err := evalPrimary(ctx, p.Primary, depth)
	if err != nil {
		return nil, err
	}
	return applyTrailers(ctx, base, p.Ops, depth)
}

// evalLogCall implements the bare log(...) builtin as a real expression —
// not just a top-level statement — so it can appear anywhere an expression
// can, including a `tool` method body (`print_json(json: any) -> log(json)`).
// It always evaluates to nil. This is the one place this interpreter prints
// a value the .mh author explicitly asked to see; memory.get() itself stays
// silent unless wrapped in log(...). Bare log(...) carries no level — it
// predates log.info/log.warn/log.error (nativeOpCall, tool.go) and keeps
// printing unprefixed for backward compatibility with existing .mh scripts
// and their test assertions.
// isBoundVar reports whether name is a step-local (`var`) or pipeline-level
// variable currently in scope. When it is, a `name.method(...)` call is an
// ordinary value-method call on whatever that variable holds — it must NOT
// be mistaken for a call on a same-named declared construct (memory, tool,
// mcp_server, ...) or hit the "memory %q not found" typo guard, which would
// otherwise shadow value methods like `list.append(x)` or `obj.get(k, d)`.
func isBoundVar(ctx *evalCtx, name string) bool {
	if ctx == nil {
		return false
	}
	if _, ok := ctx.env[name]; ok {
		return true
	}
	_, ok := ctx.pipelineEnv[name]
	return ok
}

func evalLogCall(ctx *evalCtx, args []*ast.Argument, depth int) (any, error) {
	values, err := evalLogArgs(ctx, args, depth)
	if err != nil {
		return nil, err
	}
	writeLog(ctx, "", values)
	return nil, nil
}

// evalLogArgs evaluates a log call's arguments in order, shared by bare
// log(...) (evalLogCall) and the leveled log.info/warn/error ops
// (nativeOpCall, tool.go).
func evalLogArgs(ctx *evalCtx, args []*ast.Argument, depth int) ([]any, error) {
	values := make([]any, 0, len(args))
	for _, arg := range args {
		v, err := evalExprAt(ctx, arg.Value, depth)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}

// writeLog formats values the same space-joined way for every log variant
// and writes one line to ctx.out, prefixing it with "[LEVEL] " when level is
// non-empty (log.info/warn/error) and leaving it bare when not (log(...)).
func writeLog(ctx *evalCtx, level string, values []any) {
	line := joinValues(values)
	if level != "" {
		line = "[" + level + "] " + line
	}
	// Mask any resolved credential (env("…_TOKEN"), an http auth token, …)
	// that made it into a logged value.
	fmt.Fprintln(ctx.out, auth.Redact(line))
}

// joinValues formats values the same space-joined way every log/fail
// variant does — the one formatting rule shared by writeLog and
// evalFailCall's error message.
func joinValues(values []any) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = formatValue(v)
	}
	return strings.Join(parts, " ")
}

// evalFailCall implements the fail(...) builtin: unlike log(...), which
// only traces to ctx.out and always evaluates to nil, fail(...) evaluates
// its space-joined arguments into a message and returns a real Go error in
// place of a value. That error unwinds exactly like any other expression
// evaluation error (division by zero, a missing agent, an unhandled
// fs.read failure, ...) — up through execBlock/execIf/execTry, RunStep, the
// pipeline Runner, and cli.Run — so an uncaught fail(...) makes `mhl run`
// report it and cmd/mhl's main() os.Exit(1). A `try { fail(...) } catch
// (e) { ... }` catches it the same way it already catches any other
// error (execTry, exec.go), since fail(...) is a plain error, not a
// control-flow signal like break/return/goto (isControlSignal, exec.go).
// This is the deliberate counterpart to a pipeline failing by accident: a
// `break "reason"` always exits 0 (runPipeline, cli.go) even with a
// reason, so fail(...) is what a .mh author reaches for when a run must be
// reported as failed to whatever invoked `mhl run`.
func evalFailCall(ctx *evalCtx, args []*ast.Argument, depth int) (any, error) {
	values, err := evalLogArgs(ctx, args, depth)
	if err != nil {
		return nil, err
	}
	return nil, errors.New(joinValues(values))
}

// evalEnvCall implements the env(name) builtin: it reads the OS environment
// variable named by its single string argument, returning "" when unset —
// the same "" for absent vs. empty distinction os.Getenv already makes, not
// a separate null. Available anywhere an expression can appear (a pipeline
// step, a describe block's `if`, a tool method body, ...), so a program can
// gate behavior on an operator-supplied flag, e.g. skipping a describe block
// that calls a real, paid external API unless `ENABLE_LLM_CALL=1` is set.
// typeBuiltinWants maps each `is_*` runtime-introspection builtin to the
// typeName it tests for. `type_of` is handled separately (it returns the
// name rather than a bool).
var typeBuiltinWants = map[string]string{
	"is_string": "string",
	"is_number": "number",
	"is_bool":   "bool",
	"is_array":  "array",
	"is_object": "object",
	"is_null":   "null",
}

// evalTypeBuiltinCall handles the bare, receiver-less introspection calls:
// `type_of(x)` returns x's kind as a string ("string", "number", "bool",
// "array", "object", "null", "function", "task"); each `is_<kind>(x)`
// returns a bool. handled is false when name is not one of them, so the
// caller falls through to its normal resolution.
func evalTypeBuiltinCall(ctx *evalCtx, name string, args []*ast.Argument, depth int) (result any, handled bool, err error) {
	want, isPred := typeBuiltinWants[name]
	if name != "type_of" && !isPred {
		return nil, false, nil
	}
	if len(args) != 1 {
		return nil, true, fmt.Errorf("%s() requires exactly one argument", name)
	}
	v, err := evalExprAt(ctx, args[0].Value, depth)
	if err != nil {
		return nil, true, err
	}
	if name == "type_of" {
		return typeName(v), true, nil
	}
	return typeName(v) == want, true, nil
}

func evalEnvCall(ctx *evalCtx, args []*ast.Argument, depth int) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("env() requires exactly one argument (the variable name)")
	}
	v, err := evalExprAt(ctx, args[0].Value, depth)
	if err != nil {
		return nil, err
	}
	name, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("env() requires a string argument, got %s", typeName(v))
	}
	value := os.Getenv(name)
	// A read through a credential-shaped name (…_TOKEN, …_SECRET, …API_KEY)
	// is treated as a secret: register it so it is masked in logs, error
	// output and persisted checkpoints even when the .mh author never went
	// through a declared credential reference.
	if value != "" && auth.LooksSecretName(name) {
		auth.Register(value)
	}
	return value, nil
}

// applyTrailers applies a chain of member-access/call/index trailers
// against an already-evaluated value. A Member trailer immediately followed
// by a Call trailer is a built-in value method (see callValueMethod) —
// size(), is_empty(), get_index(), index_of(), keys(), and values() today —
// consuming both trailers together; a Member trailer on its own reads a
// field out of a map[string]any (the shape both object literals and
// memory.get() results take). A bare Call trailer (no preceding Member)
// invokes v as a closure when it is one (see callClosure) — that's what
// makes `predicate(item)` work when predicate is a variable holding a
// Lambda's value — and is an error for any other value, since there's
// nothing else callable. An Index trailer is `container[i]` — for an
// array, sugar for get_index(i); for an object, a *dynamic* field read by
// a runtime-computed string key (unlike the static Member trailer above,
// which only ever accepts a literal identifier known at parse time) — see
// indexRead/resolveIndexKey below. Either form, unlike the method form,
// also doubles as an assignable target (see execAssign, exec.go).
func applyTrailers(ctx *evalCtx, base any, ops []*ast.Trailer, depth int) (any, error) {
	v := base
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		switch {
		case op.Member != "" && i+1 < len(ops) && ops[i+1].Call != nil:
			// Optional chaining: `x?.method()` on a null `x` collapses the
			// whole rest of the chain to null instead of raising.
			if op.Optional && v == nil {
				return nil, nil
			}
			// A *toolRef/*mcpServerRef is what an agent's `before`/`after`
			// hook navigates to off its `tool`/`mcp` map parameter (e.g.
			// `tool.execution.read_file(...)`, `mcp.GitHub.call(...)` —
			// agent_hooks.go): dispatch it into the exact same
			// evalToolCall/evalMCPServerCall a declared name's own two-level
			// `name.member(...)` fast path (evalPostfix) calls into, rather
			// than callValueMethod below, which has no ctx/raw *ast.Call to
			// give them (named arguments, ctx.prog lookups).
			switch ref := v.(type) {
			case *toolRef:
				if ref.allowedMethods != nil && !ref.allowedMethods[op.Member] {
					return nil, fmt.Errorf("tool %q: method %q is not in this agent's declared tools: scope", ref.tool.Name, op.Member)
				}
				result, err := evalToolCall(ctx, ref.tool, op.Member, ops[i+1].Call, depth)
				if err != nil {
					return nil, err
				}
				v = result
				i++
				continue
			case *mcpServerRef:
				result, err := evalMCPServerCall(ctx, ref.server, op.Member, ops[i+1].Call, depth)
				if err != nil {
					return nil, err
				}
				v = result
				i++
				continue
			}
			args, err := evalPositionalValues(ctx, ops[i+1].Call, depth)
			if err != nil {
				return nil, err
			}
			v, err = callValueMethod(v, op.Member, args, depth)
			if err != nil {
				return nil, err
			}
			i++ // the Call trailer was consumed together with its Member
		case op.Member != "":
			// Optional chaining: `x?.name` yields null (and skips the rest
			// of the chain) when `x` is null, is not an object at all, or is
			// an object with no such field, rather than raising.
			if op.Optional && v == nil {
				return nil, nil
			}
			if h, ok := v.(*spawnHandle); ok {
				next, err := handleField(h, op.Member)
				if err != nil {
					return nil, err
				}
				v = next
				break
			}
			m, ok := v.(map[string]any)
			if !ok {
				if op.Optional {
					return nil, nil
				}
				return nil, fmt.Errorf("cannot access field %q on a %s value", op.Member, typeName(v))
			}
			next, ok := m[op.Member]
			if !ok {
				if op.Optional {
					return nil, nil
				}
				return nil, fmt.Errorf("field %q not found", op.Member)
			}
			v = next
		case op.OptIndex != nil:
			// `x?.[key]` — optional dynamic index. A null or non-indexable
			// receiver, an out-of-range array index, or a missing object key
			// all yield null and skip the rest of the chain, exactly like
			// `x?.name`. A wrong key *type* for the receiver (a string key
			// into an array, say) is still a real error.
			switch v.(type) {
			case []any, map[string]any:
				got, present, err := optionalIndexRead(ctx, v, op.OptIndex, depth)
				if err != nil {
					return nil, err
				}
				if !present {
					return nil, nil
				}
				v = got
			default: // null, or a scalar that cannot be indexed
				return nil, nil
			}
		case op.Call != nil:
			closure, ok := v.(*Closure)
			if !ok {
				return nil, fmt.Errorf("value is not callable")
			}
			result, err := callClosure(ctx, closure, op.Call, depth)
			if err != nil {
				return nil, err
			}
			v = result
		case op.Index != nil:
			next, err := indexRead(ctx, v, op.Index, depth)
			if err != nil {
				return nil, err
			}
			v = next
		case op.Slice != nil:
			sliced, err := sliceArray(ctx, v, op.Slice, depth)
			if err != nil {
				return nil, err
			}
			v = sliced
		}
	}
	return v, nil
}

// resolveIndexKey evaluates indexExpr and validates it against receiver's
// type, returning a key ready to use against that receiver: an in-bounds
// int for a []any, or a string for a map[string]any — the shared
// "right key type, and in range for an array" check behind both
// `container[i]` reads (indexRead) and `container[i] = value` writes
// (indexWrite / execAssign, exec.go). It has its own error text rather than
// reusing get_index()'s (callValueMethod) so that method's existing,
// test-asserted error strings stay untouched.
func resolveIndexKey(ctx *evalCtx, receiver any, indexExpr *ast.Expr, depth int) (any, error) {
	idxVal, err := evalExprAt(ctx, indexExpr, depth)
	if err != nil {
		return nil, err
	}
	switch r := receiver.(type) {
	case []any:
		idxF, ok := idxVal.(float64)
		if !ok {
			return nil, fmt.Errorf("array index must be a number, got %s", typeName(idxVal))
		}
		idx := int(idxF)
		if float64(idx) != idxF {
			return nil, fmt.Errorf("array index must be an integer, got %v", idxF)
		}
		if idx < 0 || idx >= len(r) {
			return nil, fmt.Errorf("index %d out of range (size %d)", idx, len(r))
		}
		return idx, nil
	case map[string]any:
		key, ok := idxVal.(string)
		if !ok {
			return nil, fmt.Errorf("object key must be a string, got %s", typeName(idxVal))
		}
		return key, nil
	default:
		return nil, fmt.Errorf("cannot index a %s value", typeName(receiver))
	}
}

// indexRead resolves `container[i]` for a read — an array element by
// integer index, or an object field by a dynamic (runtime-computed) string
// key, unlike the static `.field` trailer which only accepts a literal
// identifier known at parse time. See resolveIndexKey for key validation.
func indexRead(ctx *evalCtx, receiver any, indexExpr *ast.Expr, depth int) (any, error) {
	key, err := resolveIndexKey(ctx, receiver, indexExpr, depth)
	if err != nil {
		return nil, err
	}
	switch r := receiver.(type) {
	case []any:
		return r[key.(int)], nil
	case map[string]any:
		k := key.(string)
		v, ok := r[k]
		if !ok {
			return nil, fmt.Errorf("field %q not found", k)
		}
		return v, nil
	default:
		panic("resolveIndexKey validated receiver type")
	}
}

// optionalIndexRead backs the `x?.[key]` trailer. receiver is already known
// to be a []any or a map[string]any (the caller short-circuits every other
// type to null). It reports present=false — never an error — for an
// out-of-range array index or a missing object key, so the caller can skip
// the rest of the chain the same way `x?.name` does. A key whose *type* is
// wrong for the receiver is still returned as an error.
func optionalIndexRead(ctx *evalCtx, receiver any, indexExpr *ast.Expr, depth int) (value any, present bool, err error) {
	idxVal, err := evalExprAt(ctx, indexExpr, depth)
	if err != nil {
		return nil, false, err
	}
	switch r := receiver.(type) {
	case []any:
		idxF, ok := idxVal.(float64)
		if !ok {
			return nil, false, fmt.Errorf("array index must be a number, got %s", typeName(idxVal))
		}
		idx := int(idxF)
		if float64(idx) != idxF {
			return nil, false, fmt.Errorf("array index must be an integer, got %v", idxF)
		}
		if idx < 0 || idx >= len(r) {
			return nil, false, nil
		}
		return r[idx], true, nil
	case map[string]any:
		key, ok := idxVal.(string)
		if !ok {
			return nil, false, fmt.Errorf("object key must be a string, got %s", typeName(idxVal))
		}
		v, ok := r[key]
		return v, ok, nil
	default:
		return nil, false, nil
	}
}

// indexWrite resolves `container[i] = value` for a write — mutating an
// array element in place or setting/overwriting an object field by a
// dynamic key. Both []any and map[string]any are Go reference types, so
// mutating through receiver here is visible through every alias of the
// same value, exactly like the array-only behavior this generalizes (see
// execAssign, exec.go).
func indexWrite(ctx *evalCtx, receiver any, indexExpr *ast.Expr, value any, depth int) error {
	key, err := resolveIndexKey(ctx, receiver, indexExpr, depth)
	if err != nil {
		return err
	}
	switch r := receiver.(type) {
	case []any:
		r[key.(int)] = value
		return nil
	case map[string]any:
		r[key.(string)] = value
		return nil
	default:
		panic("resolveIndexKey validated receiver type")
	}
}

// sliceArray evaluates a range-index trailer (`numbers[lo..hi]`) against
// receiver, returning a new array holding the selected elements — a copy,
// not a view, so mutating the result never aliases receiver's backing
// array. Either bound may be omitted (nil means "from the start" / "to the
// end") and either may be `^`-prefixed to count backward from the end.
// Unlike indexArray's single-element read, an out-of-range bound is clamped
// to [0, len(arr)] rather than erroring — the common scripting-language
// convention for slicing (e.g. `numbers[3..100]` on a 7-element array just
// returns everything from index 3 on).
func sliceArray(ctx *evalCtx, receiver any, sl *ast.Slice, depth int) ([]any, error) {
	arr, ok := receiver.([]any)
	if !ok {
		return nil, fmt.Errorf("cannot index a %s value", typeName(receiver))
	}
	n := len(arr)
	lo, err := sliceBoundValue(ctx, sl.Low, 0, n, depth)
	if err != nil {
		return nil, err
	}
	hi, err := sliceBoundValue(ctx, sl.High, n, n, depth)
	if err != nil {
		return nil, err
	}
	if lo < 0 {
		lo = 0
	}
	if hi > n {
		hi = n
	}
	if lo > hi {
		lo = hi
	}
	out := make([]any, hi-lo)
	copy(out, arr[lo:hi])
	return out, nil
}

// sliceBoundValue resolves an optional slice bound to a concrete array
// index: def when bound is nil (the side of '..' was omitted), otherwise
// the bound's evaluated integer value, or size-minus-that-value when the
// bound was `^`-prefixed (counted from the end).
func sliceBoundValue(ctx *evalCtx, bound *ast.SliceBound, def, size, depth int) (int, error) {
	if bound == nil {
		return def, nil
	}
	v, err := evalExprAt(ctx, bound.Value, depth)
	if err != nil {
		return 0, err
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("slice bound must be a number, got %s", typeName(v))
	}
	idx := int(f)
	if float64(idx) != f {
		return 0, fmt.Errorf("slice bound must be an integer, got %v", f)
	}
	if bound.FromEnd {
		idx = size - idx
	}
	return idx, nil
}

// callValueMethod dispatches a built-in method call on an already-evaluated
// value — reached both for a chained call (`memory.get(...).size()`) and
// for one on a plain variable (`objs.size()`), since both paths funnel
// through applyTrailers. size() returns the element/entry/byte count of an
// array, object, or string; is_empty() is the same check as size() == 0,
// added alongside it since it's free once size() exists and is what
// language-design.md's own pipeline example (§8) expects.
func callValueMethod(receiver any, name string, args []any, depth int) (any, error) {
	size := func() (int, error) {
		switch v := receiver.(type) {
		case []any:
			return len(v), nil
		case map[string]any:
			return len(v), nil
		case string:
			return len(v), nil
		default:
			return 0, fmt.Errorf("%s() is not defined for a %s value", name, typeName(receiver))
		}
	}
	switch name {
	case "size":
		if len(args) != 0 {
			return nil, fmt.Errorf("size() takes no arguments")
		}
		n, err := size()
		if err != nil {
			return nil, err
		}
		return float64(n), nil
	case "is_empty":
		if len(args) != 0 {
			return nil, fmt.Errorf("is_empty() takes no arguments")
		}
		n, err := size()
		if err != nil {
			return nil, err
		}
		return n == 0, nil
	case "equals", "deep_equal":
		// Defined for every value kind (not just containers): a type-aware,
		// order-sensitive deep comparison — the same equality `==` and the
		// `are_equal` assertion use.
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() requires exactly one argument (the value to compare against)", name)
		}
		return reflect.DeepEqual(receiver, args[0]), nil
	case "get_index":
		if len(args) != 1 {
			return nil, fmt.Errorf("get_index() requires exactly one argument (the index)")
		}
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("get_index() is not defined for a %s value", typeName(receiver))
		}
		idxF, ok := args[0].(float64)
		if !ok {
			return nil, fmt.Errorf("get_index() argument must be a number, got %s", typeName(args[0]))
		}
		idx := int(idxF)
		if float64(idx) != idxF {
			return nil, fmt.Errorf("get_index() argument must be an integer, got %v", idxF)
		}
		if idx < 0 || idx >= len(arr) {
			return nil, fmt.Errorf("get_index(%d) out of range (size %d)", idx, len(arr))
		}
		return arr[idx], nil
	case "index_of":
		if len(args) != 1 {
			return nil, fmt.Errorf("index_of() requires exactly one argument (the value to search for)")
		}
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("index_of() is not defined for a %s value", typeName(receiver))
		}
		for i, item := range arr {
			if reflect.DeepEqual(item, args[0]) {
				return float64(i), nil
			}
		}
		return float64(-1), nil
	case "keys":
		if len(args) != 0 {
			return nil, fmt.Errorf("keys() takes no arguments")
		}
		obj, ok := receiver.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("keys() is not defined for a %s value", typeName(receiver))
		}
		return objectKeys(obj), nil
	case "values":
		if len(args) != 0 {
			return nil, fmt.Errorf("values() takes no arguments")
		}
		obj, ok := receiver.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("values() is not defined for a %s value", typeName(receiver))
		}
		names := sortedKeyNames(obj)
		values := make([]any, len(names))
		for i, k := range names {
			values[i] = obj[k]
		}
		return values, nil
	case "split":
		s, ok := receiver.(string)
		if !ok {
			return nil, fmt.Errorf("split() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("split() requires exactly one argument (the separator)")
		}
		sep, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("split() argument must be a string, got %s", typeName(args[0]))
		}
		parts := strings.Split(s, sep)
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = p
		}
		return out, nil
	case "replace":
		s, ok := receiver.(string)
		if !ok {
			return nil, fmt.Errorf("replace() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 2 {
			return nil, fmt.Errorf("replace() requires exactly two arguments (old, new)")
		}
		oldS, ok1 := args[0].(string)
		newS, ok2 := args[1].(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("replace() arguments must be strings")
		}
		return strings.ReplaceAll(s, oldS, newS), nil
	case "contains":
		if len(args) != 1 {
			return nil, fmt.Errorf("contains() requires exactly one argument")
		}
		switch v := receiver.(type) {
		case string:
			sub, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("contains() argument must be a string, got %s", typeName(args[0]))
			}
			return strings.Contains(v, sub), nil
		case []any:
			for _, item := range v {
				if reflect.DeepEqual(item, args[0]) {
					return true, nil
				}
			}
			return false, nil
		default:
			return nil, fmt.Errorf("contains() is not defined for a %s value", typeName(receiver))
		}
	case "starts_with":
		s, ok := receiver.(string)
		if !ok {
			return nil, fmt.Errorf("starts_with() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("starts_with() requires exactly one argument")
		}
		prefix, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("starts_with() argument must be a string, got %s", typeName(args[0]))
		}
		return strings.HasPrefix(s, prefix), nil
	case "ends_with":
		s, ok := receiver.(string)
		if !ok {
			return nil, fmt.Errorf("ends_with() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("ends_with() requires exactly one argument")
		}
		suffix, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("ends_with() argument must be a string, got %s", typeName(args[0]))
		}
		return strings.HasSuffix(s, suffix), nil
	case "trim":
		s, ok := receiver.(string)
		if !ok {
			return nil, fmt.Errorf("trim() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 0 {
			return nil, fmt.Errorf("trim() takes no arguments")
		}
		return strings.TrimSpace(s), nil
	case "to_upper":
		s, ok := receiver.(string)
		if !ok {
			return nil, fmt.Errorf("to_upper() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 0 {
			return nil, fmt.Errorf("to_upper() takes no arguments")
		}
		return strings.ToUpper(s), nil
	case "to_lower":
		s, ok := receiver.(string)
		if !ok {
			return nil, fmt.Errorf("to_lower() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 0 {
			return nil, fmt.Errorf("to_lower() takes no arguments")
		}
		return strings.ToLower(s), nil
	case "substring":
		s, ok := receiver.(string)
		if !ok {
			return nil, fmt.Errorf("substring() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 2 {
			return nil, fmt.Errorf("substring() requires exactly two arguments (start, end)")
		}
		start, end, err := substringBounds(args[0], args[1], len(s))
		if err != nil {
			return nil, err
		}
		return s[start:end], nil
	case "filter":
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("filter() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("filter() requires exactly one argument (a predicate)")
		}
		predicate, ok := args[0].(*Closure)
		if !ok {
			return nil, fmt.Errorf("filter() argument must be a lambda, got %s", typeName(args[0]))
		}
		out := make([]any, 0, len(arr))
		for _, item := range arr {
			result, err := invokeClosureWithValues(predicate, []any{item}, depth)
			if err != nil {
				return nil, err
			}
			passed, ok := result.(bool)
			if !ok {
				return nil, fmt.Errorf("filter() predicate must return a boolean, got %s", typeName(result))
			}
			if passed {
				out = append(out, item)
			}
		}
		return out, nil
	case "find":
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("find() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("find() requires exactly one argument (a predicate)")
		}
		predicate, ok := args[0].(*Closure)
		if !ok {
			return nil, fmt.Errorf("find() argument must be a lambda, got %s", typeName(args[0]))
		}
		for _, item := range arr {
			result, err := invokeClosureWithValues(predicate, []any{item}, depth)
			if err != nil {
				return nil, err
			}
			passed, ok := result.(bool)
			if !ok {
				return nil, fmt.Errorf("find() predicate must return a boolean, got %s", typeName(result))
			}
			if passed {
				return item, nil
			}
		}
		return nil, nil
	case "sort_by":
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("sort_by() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("sort_by() requires exactly one argument (a key function)")
		}
		keyFn, ok := args[0].(*Closure)
		if !ok {
			return nil, fmt.Errorf("sort_by() argument must be a lambda, got %s", typeName(args[0]))
		}
		return sortByKey(arr, keyFn, depth)
	case "map":
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("map() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("map() requires exactly one argument (a transform lambda)")
		}
		fn, ok := args[0].(*Closure)
		if !ok {
			return nil, fmt.Errorf("map() argument must be a lambda, got %s", typeName(args[0]))
		}
		out := make([]any, len(arr))
		for i, item := range arr {
			r, err := invokeClosureWithValues(fn, []any{item}, depth)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case "reduce":
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("reduce() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 2 {
			return nil, fmt.Errorf("reduce() requires exactly two arguments (a combiner lambda and an initial value)")
		}
		fn, ok := args[0].(*Closure)
		if !ok {
			return nil, fmt.Errorf("reduce() first argument must be a lambda, got %s", typeName(args[0]))
		}
		acc := args[1]
		for _, item := range arr {
			r, err := invokeClosureWithValues(fn, []any{acc, item}, depth)
			if err != nil {
				return nil, err
			}
			acc = r
		}
		return acc, nil
	case "any", "all":
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("%s() is not defined for a %s value", name, typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() requires exactly one argument (a predicate)", name)
		}
		pred, ok := args[0].(*Closure)
		if !ok {
			return nil, fmt.Errorf("%s() argument must be a lambda, got %s", name, typeName(args[0]))
		}
		for _, item := range arr {
			r, err := invokeClosureWithValues(pred, []any{item}, depth)
			if err != nil {
				return nil, err
			}
			b, ok := r.(bool)
			if !ok {
				return nil, fmt.Errorf("%s() predicate must return a boolean, got %s", name, typeName(r))
			}
			if name == "any" && b {
				return true, nil
			}
			if name == "all" && !b {
				return false, nil
			}
		}
		return name == "all", nil
	case "append":
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("append() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("append() requires exactly one argument (the value to add)")
		}
		// A fresh slice — the receiver array is never mutated, matching the
		// copy-on-combine semantics of the `+` array operator.
		out := make([]any, 0, len(arr)+1)
		out = append(out, arr...)
		out = append(out, args[0])
		return out, nil
	case "join":
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("join() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("join() requires exactly one argument (the separator string)")
		}
		sep, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("join() separator must be a string, got %s", typeName(args[0]))
		}
		parts := make([]string, len(arr))
		for i, item := range arr {
			parts[i] = formatValue(item)
		}
		return strings.Join(parts, sep), nil
	case "unique":
		arr, ok := receiver.([]any)
		if !ok {
			return nil, fmt.Errorf("unique() is not defined for a %s value", typeName(receiver))
		}
		if len(args) != 0 {
			return nil, fmt.Errorf("unique() takes no arguments")
		}
		out := make([]any, 0, len(arr))
		for _, item := range arr {
			seen := false
			for _, kept := range out {
				if reflect.DeepEqual(item, kept) {
					seen = true
					break
				}
			}
			if !seen {
				out = append(out, item)
			}
		}
		return out, nil
	case "get":
		obj, ok := receiver.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("get() is not defined for a %s value", typeName(receiver))
		}
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("get() requires the key and an optional default (1 or 2 arguments)")
		}
		key, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("get() key must be a string, got %s", typeName(args[0]))
		}
		if v, present := obj[key]; present {
			return v, nil
		}
		if len(args) == 2 {
			return args[1], nil
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("%s value has no method %q", typeName(receiver), name)
	}
}

// substringBounds validates rawStart/rawEnd (already-evaluated method
// arguments) as an integer [start, end) byte range within a string of the
// given length — the same integer/bounds validation get_index() and slice
// bounds (sliceBoundValue) already apply, just for a two-ended range
// instead of one index.
func substringBounds(rawStart, rawEnd any, length int) (int, int, error) {
	toIndex := func(v any, label string) (int, error) {
		f, ok := v.(float64)
		if !ok {
			return 0, fmt.Errorf("substring() %s must be a number, got %s", label, typeName(v))
		}
		n := int(f)
		if float64(n) != f {
			return 0, fmt.Errorf("substring() %s must be an integer, got %v", label, f)
		}
		return n, nil
	}
	start, err := toIndex(rawStart, "start")
	if err != nil {
		return 0, 0, err
	}
	end, err := toIndex(rawEnd, "end")
	if err != nil {
		return 0, 0, err
	}
	if start < 0 || end > length || start > end {
		return 0, 0, fmt.Errorf("substring(%d, %d) out of range (length %d)", start, end, length)
	}
	return start, end, nil
}

// sortByKey returns a new array with arr's elements sorted by the value
// keyFn produces for each (evaluated once per element, not per comparison
// — same "decorate, sort, undecorate" shape as Schwartzian transform, so a
// key function with side effects still only runs len(arr) times). Every
// key must be a number or every key must be a string; mixing the two, or
// any other key type, is an error rather than an arbitrary tie-break.
func sortByKey(arr []any, keyFn *Closure, depth int) ([]any, error) {
	type pair struct {
		item any
		key  any
	}
	pairs := make([]pair, len(arr))
	for i, item := range arr {
		key, err := invokeClosureWithValues(keyFn, []any{item}, depth)
		if err != nil {
			return nil, err
		}
		switch key.(type) {
		case float64, string:
		default:
			return nil, fmt.Errorf("sort_by() key must be a number or string, got %s", typeName(key))
		}
		pairs[i] = pair{item: item, key: key}
	}
	var mismatch error
	sort.SliceStable(pairs, func(i, j int) bool {
		switch a := pairs[i].key.(type) {
		case float64:
			b, ok := pairs[j].key.(float64)
			if !ok && mismatch == nil {
				mismatch = fmt.Errorf("sort_by() keys must all be the same type (number or string)")
			}
			return a < b
		case string:
			b, ok := pairs[j].key.(string)
			if !ok && mismatch == nil {
				mismatch = fmt.Errorf("sort_by() keys must all be the same type (number or string)")
			}
			return a < b
		default:
			return false
		}
	})
	if mismatch != nil {
		return nil, mismatch
	}
	out := make([]any, len(pairs))
	for i, p := range pairs {
		out[i] = p.item
	}
	return out, nil
}

// sortedKeyNames returns obj's keys sorted alphabetically — a Go map has no
// defined iteration order, and this is what keeps keys()/values() (and the
// value each entry in values() lines up with) deterministic across calls,
// matching the alphabetical key order formatValue's json.Marshal already
// produces for an object logged directly.
func sortedKeyNames(obj map[string]any) []string {
	names := make([]string, 0, len(obj))
	for k := range obj {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// objectKeys is sortedKeyNames's result as an MHL array value (any, not
// string, per Env/Value's dynamic typing).
func objectKeys(obj map[string]any) []any {
	names := sortedKeyNames(obj)
	keys := make([]any, len(names))
	for i, k := range names {
		keys[i] = k
	}
	return keys
}

func evalPrimary(ctx *evalCtx, p *ast.Primary, depth int) (any, error) {
	switch {
	case p.Str != nil:
		return interpolate(ctx, *p.Str)
	case p.MultiStr != nil:
		return interpolate(ctx, *p.MultiStr)
	case p.Number != nil:
		return *p.Number, nil
	case p.Bool != nil:
		return *p.Bool == "true", nil
	case p.Null:
		return nil, nil
	case p.Array != nil:
		if depth >= maxValueDepth {
			return nil, fmt.Errorf("value nesting exceeds the maximum depth of %d", maxValueDepth)
		}
		items := make([]any, 0, len(p.Array.Items))
		for _, item := range p.Array.Items {
			v, err := evalExprAt(ctx, item, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		return items, nil
	case p.Object != nil:
		if depth >= maxValueDepth {
			return nil, fmt.Errorf("value nesting exceeds the maximum depth of %d", maxValueDepth)
		}
		obj := make(map[string]any, len(p.Object.Fields))
		for _, f := range p.Object.Fields {
			var key string
			switch {
			case f.KeyStr != nil:
				key = *f.KeyStr
			case f.KeyIdent != nil:
				key = *f.KeyIdent
			}
			v, err := evalExprAt(ctx, f.Value, depth+1)
			if err != nil {
				return nil, err
			}
			obj[key] = v
		}
		return obj, nil
	case p.Sub != nil:
		return evalExprAt(ctx, p.Sub, depth)
	case p.Lambda != nil:
		return newClosure(ctx, p.Lambda), nil
	case p.IfExpr != nil:
		return evalIfExpr(ctx, p.IfExpr, depth)
	case p.Ident != "":
		if v, ok := ctx.env[p.Ident]; ok {
			return v, nil
		}
		if v, ok := ctx.pipelineEnv[p.Ident]; ok {
			return v, nil
		}
		if isMemVar(ctx, p.Ident) {
			return readMemVar(ctx, p.Ident)
		}
		if isContextRef(ctx, p.Ident) {
			return contextSnapshot(ctx.cctx), nil
		}
		return nil, fmt.Errorf("undefined variable %q", p.Ident)
	case p.Agent != nil:
		return nil, fmt.Errorf("inline agent declarations cannot be used as an expression value")
	case p.Duration != "":
		return nil, fmt.Errorf("duration values are not supported in expressions")
	default:
		return nil, fmt.Errorf("unsupported expression")
	}
}

// evalPositionalValues evaluates call's arguments, in order, via evalExpr —
// unlike the old literal-only positionalValues, an argument may now be a
// variable, a nested memory/agent call, or an operator expression, not just
// a bare literal.
func evalPositionalValues(ctx *evalCtx, call *ast.Call, depth int) ([]any, error) {
	values := make([]any, 0, len(call.Args))
	for _, arg := range call.Args {
		v, err := evalExprAt(ctx, arg.Value, depth)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case *Closure:
		return "function"
	case *spawnHandle:
		return "task"
	default:
		return fmt.Sprintf("%T", v)
	}
}
