package interpreter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// defaultSpawnConcurrency bounds how many spawned agent goroutines may be
// inside their underlying subprocess/HTTP call at once when a pipeline does
// not set `spawn: { max_concurrency: N }`.
const defaultSpawnConcurrency = 4

// spawnHandle is the value a `spawn name = Agent.run(...)` statement binds:
// an opaque reference to one background agent call. Its result/error are not
// readable until a `wait` (or the step's end auto-drain) has joined it; the
// field accessor in applyTrailers surfaces them as `.result`, `.ok`,
// `.error`, `.status`, `.duration_ms`.
type spawnHandle struct {
	id        int
	agentName string
	done      chan struct{}
	cancel    context.CancelFunc
	buf       *bytes.Buffer // this goroutine's stdout, flushed in spawn order on join

	mu       sync.Mutex
	result   string
	err      error
	started  time.Time
	finished time.Time
	flushed  bool
}

// finish records the outcome of the underlying agent call exactly once.
func (h *spawnHandle) finish(result string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.finished.IsZero() {
		return
	}
	h.result = result
	h.err = err
	h.finished = time.Now()
}

// flush copies this handle's captured output to out the first time it is
// called and reports whether it did (so a caller can pair it with a
// one-time "not awaited" warning). Safe to call after the goroutine has
// closed done — no more writes to buf can happen then.
func (h *spawnHandle) flush(out io.Writer) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.flushed {
		return false
	}
	h.flushed = true
	if h.buf.Len() > 0 && out != nil {
		_, _ = out.Write(h.buf.Bytes())
	}
	return true
}

// handleField resolves `handle.<field>` for the five readable fields. Before
// the handle is joined every field but `status` ("pending") reads as a zero
// value rather than erroring, so `wait any` / `wait N of` code can branch on
// `.ok` for the losers without a guard.
func handleField(h *spawnHandle, field string) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	done := !h.finished.IsZero()
	switch field {
	case "result":
		return h.result, nil
	case "ok":
		return done && h.err == nil, nil
	case "error":
		if h.err == nil {
			return "", nil
		}
		return h.err.Error(), nil
	case "status":
		switch {
		case !done:
			return "pending", nil
		case h.err == nil:
			return "done", nil
		case errors.Is(h.err, context.Canceled), errors.Is(h.err, context.DeadlineExceeded):
			return "cancelled", nil
		default:
			return "failed", nil
		}
	case "duration_ms":
		if !done {
			return float64(0), nil
		}
		return float64(h.finished.Sub(h.started).Milliseconds()), nil
	default:
		return nil, fmt.Errorf("task value has no field %q (expected result, ok, error, status, or duration_ms)", field)
	}
}

// NewSpawnSem builds the semaphore that bounds concurrent `spawn`ed agent
// calls for one pipeline run. It is created once (see cli.go) and shared by
// every step's registry, so `spawn: { max_concurrency: N }` is a
// whole-pipeline ceiling, not per step. A non-positive maxConcurrency uses
// defaultSpawnConcurrency.
func NewSpawnSem(maxConcurrency int) chan struct{} {
	if maxConcurrency <= 0 {
		maxConcurrency = defaultSpawnConcurrency
	}
	return make(chan struct{}, maxConcurrency)
}

// spawnRegistry tracks every handle created in one step execution and shares
// the run-wide concurrency semaphore. It lives on evalCtx.spawns, set only
// by RunStep, which is what confines `spawn` to a step body.
type spawnRegistry struct {
	mu      sync.Mutex
	handles []*spawnHandle
	lastID  int
	sem     chan struct{}
}

func (r *spawnRegistry) register(h *spawnHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastID++
	h.id = r.lastID
	r.handles = append(r.handles, h)
}

// spawn starts agent's `.run(...)` call on a background goroutine and
// returns its handle immediately. The parent env is deep-copied at this
// point so the goroutine never races the step's later assignments; the
// goroutine gets a cancellable context so `wait` timeout / fail-fast can
// abort the subprocess.
func (r *spawnRegistry) spawn(ctx *evalCtx, agentName string, agent *ast.Agent, call *ast.Call, depth int) *spawnHandle {
	snapshot := deepCopyEnv(ctx.env)
	for k, v := range ctx.pipelineEnv {
		if _, shadowed := snapshot[k]; !shadowed {
			snapshot[k] = deepCopyValue(v)
		}
	}

	childCtx, cancel := context.WithCancel(goctxOf(ctx))
	h := &spawnHandle{
		agentName: agentName,
		done:      make(chan struct{}),
		cancel:    cancel,
		buf:       &bytes.Buffer{},
		started:   time.Now(),
	}
	r.register(h)

	hctx := *ctx
	hctx.env = snapshot
	hctx.pipelineEnv = nil
	hctx.out = h.buf
	hctx.goctx = childCtx
	hctx.inSpawn = true
	hctx.spawns = nil

	go func() {
		defer close(h.done)
		defer func() {
			if rec := recover(); rec != nil {
				h.finish("", fmt.Errorf("spawned agent %q panicked: %v", agentName, rec))
			}
		}()

		select {
		case r.sem <- struct{}{}:
			defer func() { <-r.sem }()
		case <-childCtx.Done():
			h.finish("", childCtx.Err())
			return
		}
		if err := childCtx.Err(); err != nil {
			h.finish("", err)
			return
		}

		resp, err := runAgent(&hctx, agentName, agent, call, depth)
		h.finish(resp, err)
	}()

	return h
}

// drainAtStepEnd joins every handle the step never waited on (RunStep defers
// this), flushing their output in spawn order and printing one warning line
// per handle that failed unnoticed. When cancelPending is set — the step is
// unwinding on a genuine error — it cancels the stragglers first instead of
// blocking on work the step will never use; on a normal finish it lets them
// run to completion. Either way, no spawned goroutine outlives the step.
func (r *spawnRegistry) drainAtStepEnd(out io.Writer, cancelPending bool) {
	r.mu.Lock()
	pending := append([]*spawnHandle(nil), r.handles...)
	r.mu.Unlock()

	if cancelPending {
		for _, h := range pending {
			h.cancel()
		}
	}
	for _, h := range pending {
		<-h.done
	}
	for _, h := range pending {
		if h.flush(out) && h.err != nil && !cancelPending {
			fmt.Fprintf(out, "warning: spawned agent %q was not awaited and failed: %v\n", h.agentName, h.err)
		}
	}
}

// execSpawn runs a `spawn name = Agent.run(...)` statement: it validates the
// context and target, starts the background call, and binds the handle.
func execSpawn(ctx *evalCtx, stmt *ast.SpawnStmt) error {
	if ctx.spawns == nil {
		return fmt.Errorf("spawn is only valid inside a pipeline step")
	}
	if ctx.inSpawn {
		return fmt.Errorf("spawn %q: a spawned agent cannot spawn again", stmt.Name)
	}
	name, agent, call, ok := agentRunTarget(ctx, stmt.Call)
	if !ok {
		return fmt.Errorf("spawn %q: right-hand side must be an <Agent>.run(...) call", stmt.Name)
	}
	ctx.env[stmt.Name] = ctx.spawns.spawn(ctx, name, agent, call, 0)
	return nil
}

// agentRunTarget matches a bare `<Agent>.run(<call>)` expression — the only
// shape `spawn`'s right-hand side accepts — and resolves the agent.
func agentRunTarget(ctx *evalCtx, e *ast.Expr) (string, *ast.Agent, *ast.Call, bool) {
	pf := ast.BarePostfix(e)
	if pf == nil || pf.Primary == nil || pf.Primary.Ident == "" {
		return "", nil, nil, false
	}
	if len(pf.Ops) != 2 || pf.Ops[0].Member != "run" || pf.Ops[1].Call == nil {
		return "", nil, nil, false
	}
	agent, ok := findAgent(ctx.prog, pf.Primary.Ident)
	if !ok {
		return "", nil, nil, false
	}
	return pf.Primary.Ident, agent, pf.Ops[1].Call, true
}

// execWait runs a `wait ...` statement: it resolves the named handles, reads
// the mode and options, and delegates to waitHandles.
func execWait(ctx *evalCtx, stmt *ast.WaitStmt) error {
	if ctx.spawns == nil {
		return fmt.Errorf("wait is only valid inside a pipeline step")
	}
	if len(stmt.Names) == 0 {
		return fmt.Errorf("wait: name at least one spawned handle")
	}

	handles := make([]*spawnHandle, 0, len(stmt.Names))
	seen := make(map[string]bool, len(stmt.Names))
	for _, n := range stmt.Names {
		if seen[n] {
			return fmt.Errorf("wait: handle %q listed twice", n)
		}
		seen[n] = true
		v, ok := ctx.env[n]
		if !ok {
			return fmt.Errorf("wait: %q is not a spawned handle in this step", n)
		}
		h, ok := v.(*spawnHandle)
		if !ok {
			return fmt.Errorf("wait: %q is a %s, not a spawned handle", n, typeName(v))
		}
		handles = append(handles, h)
	}

	opts := waitOptions{mode: waitAll}
	if stmt.Any {
		opts.mode = waitAny
	}
	if stmt.Quorum != "" {
		q, err := strconv.Atoi(stmt.Quorum)
		if err != nil || q < 1 {
			return fmt.Errorf("wait: quorum count %q must be a positive integer", stmt.Quorum)
		}
		if q > len(handles) {
			return fmt.Errorf("wait %d of %d: quorum exceeds the number of handles", q, len(handles))
		}
		opts.mode = waitQuorum
		opts.quorum = q
	}

	for _, o := range stmt.Opts {
		switch o.Key {
		case "timeout":
			d, ok := ast.DurationValue(o.Value)
			if !ok {
				return fmt.Errorf("wait: timeout: must be a duration literal (e.g. 30s)")
			}
			opts.timeout = d
		case "on_error":
			s, ok := ast.StringValue(o.Value)
			if !ok {
				return fmt.Errorf("wait: on_error: must be a string")
			}
			if s != "collect" {
				return fmt.Errorf("wait: on_error: only \"collect\" is supported")
			}
			if opts.mode != waitAll {
				return fmt.Errorf("wait: on_error: \"collect\" only applies to a plain `wait`")
			}
			opts.collect = true
		}
	}

	return waitHandles(ctx, handles, opts)
}

type waitMode int

const (
	waitAll waitMode = iota
	waitAny
	waitQuorum
)

type waitOptions struct {
	mode    waitMode
	quorum  int
	timeout time.Duration
	collect bool // wait-all only: never fail the step, leave outcomes on the handles
}

// waitHandles blocks until the requested join condition is met, then flushes
// every handle's captured output to ctx.out in spawn order. On timeout, on a
// fail-fast error, or once a decisive result is in, it cancels the handles
// that are still running before returning.
func waitHandles(ctx *evalCtx, handles []*spawnHandle, opts waitOptions) error {
	if len(handles) == 0 {
		return nil
	}

	base := goctxOf(ctx)
	tctx := base
	if opts.timeout > 0 {
		var tcancel context.CancelFunc
		tctx, tcancel = context.WithTimeout(base, opts.timeout)
		defer tcancel()
	}

	events := make(chan int, len(handles))
	for i, h := range handles {
		go func(i int, h *spawnHandle) {
			<-h.done
			events <- i
		}(i, h)
	}

	finalize := func(retErr error) error {
		cancelAll(handles)
		for _, h := range handles {
			<-h.done
		}
		flushInOrder(handles, ctx.out)
		return retErr
	}

	timedOut := func() error {
		return finalize(fmt.Errorf("wait: timed out after %s", opts.timeout))
	}

	remaining := len(handles)
	var successes, failures int

	for remaining > 0 {
		select {
		case <-tctx.Done():
			if errors.Is(tctx.Err(), context.DeadlineExceeded) {
				return timedOut()
			}
			return finalize(tctx.Err())
		case i := <-events:
			remaining--
			h := handles[i]
			ok := h.err == nil
			if ok {
				successes++
			} else {
				failures++
			}

			switch opts.mode {
			case waitAll:
				if !opts.collect && !ok {
					return finalize(fmt.Errorf("%s.spawn: %w", h.agentName, h.err))
				}
			case waitAny:
				if ok {
					return finalize(nil)
				}
			case waitQuorum:
				if successes >= opts.quorum {
					return finalize(nil)
				}
				if failures > len(handles)-opts.quorum {
					return finalize(fmt.Errorf("wait %d of %d: quorum impossible, %d already failed",
						opts.quorum, len(handles), failures))
				}
			}
		}
	}

	// Every handle finished without a decisive early return.
	switch opts.mode {
	case waitAny:
		return finalize(fmt.Errorf("wait any: all %d spawned agents failed", len(handles)))
	case waitQuorum:
		if successes < opts.quorum {
			return finalize(fmt.Errorf("wait %d of %d: only %d succeeded", opts.quorum, len(handles), successes))
		}
	}
	flushInOrder(handles, ctx.out)
	return nil
}

func cancelAll(handles []*spawnHandle) {
	for _, h := range handles {
		h.cancel()
	}
}

func flushInOrder(handles []*spawnHandle, out io.Writer) {
	for _, h := range handles {
		h.flush(out)
	}
}

func deepCopyEnv(env Env) Env {
	out := make(Env, len(env))
	for k, v := range env {
		out[k] = deepCopyValue(v)
	}
	return out
}

// deepCopyValue clones the JSON-shaped part of a value (arrays and objects)
// so a spawned goroutine reading it cannot observe the parent step mutating
// the same structure afterwards. Scalars are immutable; a *Closure or other
// interpreter pointer is shared by reference (documented limitation).
func deepCopyValue(v any) any {
	switch t := v.(type) {
	case []any:
		c := make([]any, len(t))
		for i := range t {
			c[i] = deepCopyValue(t[i])
		}
		return c
	case map[string]any:
		c := make(map[string]any, len(t))
		for k := range t {
			c[k] = deepCopyValue(t[k])
		}
		return c
	default:
		return v
	}
}
