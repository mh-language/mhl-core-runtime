package interpreter

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// Closure is a first-class function value: a Lambda's parameters/body
// bundled with a snapshot of the Env captured at the moment the Lambda
// expression was evaluated. It's a real lexical closure in that it can
// read variables from its defining scope, not just its own bound
// parameters (unlike a tool method's always-fresh, empty-until-bound Env)
// — but the capture is by value (a shallow copy taken once, at creation),
// not a live reference to the defining scope's Env. For the motivating use
// case (a predicate created and invoked immediately, e.g.
// `Linq.where(items, (item) -> item.passes == true)`) that's
// observationally identical to capturing by reference; it only matters if
// a closure were stored and called long after its captured variables
// changed, or if its body tried to `assign` a captured name expecting that
// to leak back out — neither is supported. This avoids sharing a mutable
// map across calls, which is a much easier way to get closures subtly
// wrong than the (documented) limitation of not supporting that.
type Closure struct {
	def         *ast.Lambda
	definingCtx *evalCtx
	capturedEnv Env
}

// newClosure captures ctx's current Env (a shallow copy) for l.
func newClosure(ctx *evalCtx, l *ast.Lambda) *Closure {
	captured := make(Env, len(ctx.env))
	for k, v := range ctx.env {
		captured[k] = v
	}
	return &Closure{def: l, definingCtx: ctx, capturedEnv: captured}
}

// callClosure invokes c with call's evaluated positional arguments bound
// to its declared Params, on top of a fresh copy of its captured Env (so
// the call's parameters never pollute — and never persist past — the
// closure's own captured snapshot).
func callClosure(ctx *evalCtx, c *Closure, call *ast.Call, depth int) (any, error) {
	args, err := evalPositionalValues(ctx, call, depth)
	if err != nil {
		return nil, err
	}
	return invokeClosureWithValues(c, args, depth)
}

// invokeClosureWithValues is callClosure's binding/invocation core, split
// out so a caller that already has plain Go values in hand — not an
// *ast.Call to evaluate — can invoke a closure directly. This is what
// array.filter/sort_by/find (callValueMethod, eval.go) use to run a
// predicate once per element: there is no source-level call expression at
// that point, just the element value already sitting in a Go slice.
func invokeClosureWithValues(c *Closure, args []any, depth int) (any, error) {
	if len(args) != len(c.def.Params) {
		return nil, fmt.Errorf("closure requires %d argument(s), got %d", len(c.def.Params), len(args))
	}
	callEnv := make(Env, len(c.capturedEnv)+len(args))
	for k, v := range c.capturedEnv {
		callEnv[k] = v
	}
	for i, p := range c.def.Params {
		callEnv[p.Name] = args[i]
	}
	callCtx := &evalCtx{
		prog:        c.definingCtx.prog,
		store:       c.definingCtx.store,
		jsonStore:   c.definingCtx.jsonStore,
		out:         c.definingCtx.out,
		env:         callEnv,
		pipelineEnv: c.definingCtx.pipelineEnv,
		mem:         c.definingCtx.mem,
		cctx:        c.definingCtx.cctx,
		file:        c.definingCtx.file,
		selfTool:    c.definingCtx.selfTool,
	}
	return invokeCallable(callCtx, c.def.Body, c.def.Block, depth)
}
