package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

// asyncRun is one workflow execution started by `run/start` and tracked
// server-side so a client can poll `run/status` (which step it is on, which
// steps it has reached, the final vars) and stop it with `run/cancel`
// instead of holding the HTTP request open for the whole run.
//
// State lives under h.runsDir/.mhl/state/<id>/ via execsvc's Session scoping.
// A run that stops at a failing step keeps that state, and — when its
// workflow declares checkpoint { strategy: per_step } — `run/resume` picks
// it up from the failing step (the HITL pattern: a gate step calls
// `fail("awaiting approval")`, the operator resumes once approved).
//
// Every run is owned by its caller (httpServer.ownerOf — the verified
// principal when a TokenVerifier is configured, else the Phase-0 per-session
// hash); status, resume, cancel and list only act for a matching caller. A
// completed run is swept after sessionTTL; a resumable one is kept for the
// process lifetime so its owner binding holds, and its on-disk state is GC'd
// by runtime.PruneExpired. After a restart the in-memory run is gone, so the
// owner is persisted alongside the checkpoint (CheckpointStore.WriteOwner) and
// reconstructRun refuses to hand it to a different caller.
type asyncRun struct {
	id string
	// owner is httpServer.ownerOf(creating session); run/status, run/resume,
	// run/cancel and run/list only act for a matching caller. Set once at
	// creation (or restored on reconstruct after a restart) — immutable after.
	owner Owner
	// principal is the raw verified identity of the caller for the current
	// leg (the starter, or the resumer), surfaced to the workflow as
	// context.principal. "" without a verifier; "" on a run reconstructed
	// from disk until it is resumed.
	principal string
	tool      execsvc.Workflow
	args      map[string]any
	started   time.Time
	cancel    context.CancelFunc
	done      chan struct{}
	// logs is this run's own bounded copy of its step/log() output, for
	// run/logs. A run reconstructed from disk after a restart has an empty one
	// (its output happened in the previous process).
	logs *ringLog
	// remote is true for a run this replica did not start: it was rebuilt by
	// reconstructRun from the shared store (a checkpoint, or a live status
	// record published by the replica that is running it). run/status refreshes
	// it from the store on each poll; run/cancel signals through the store
	// instead of calling a local cancel func.
	remote bool
	// holder is the replica id that claimed this run from durable intake
	// (RunStatusRec.Holder). "" for the in-memory-only path. Published with the
	// status so another replica's run/status can tell it was claimed here.
	holder string

	mu        sync.Mutex
	state     RunState // see runstate.go for the full state machine
	step      string   // last step reached
	stepIndex int      // 1-based position of step
	stepTotal int      // pipeline's declared step count
	reached   []string
	resumable bool // a checkpoint exists that run/resume can continue from
	vars      map[string]any
	errMsg    string
	updated   time.Time
}

// --- concurrency slots -------------------------------------------------

// tryAcquireSlot takes a run slot without blocking. When concurrency is
// unlimited (h.sem == nil) it always succeeds. The returned func releases the
// slot; it is safe to call even when nothing was taken.
func (h *httpServer) tryAcquireSlot() (release func(), ok bool) {
	if h.sem == nil {
		return func() {}, true
	}
	select {
	case h.sem <- struct{}{}:
		return func() { <-h.sem }, true
	default:
		return func() {}, false
	}
}

// acquireSlot blocks until a run slot is free or ctx is done.
func (h *httpServer) acquireSlot(ctx context.Context) (release func(), ok bool) {
	if h.sem == nil {
		return func() {}, true
	}
	select {
	case h.sem <- struct{}{}:
		return func() { <-h.sem }, true
	case <-ctx.Done():
		return func() {}, false
	}
}

// acquireSlotWait is acquireSlot bounded by wait — the synchronous tools/call
// path sheds load rather than parking a client connection indefinitely.
func (h *httpServer) acquireSlotWait(ctx context.Context, wait time.Duration) (release func(), ok bool) {
	if h.sem == nil {
		return func() {}, true
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case h.sem <- struct{}{}:
		return func() { <-h.sem }, true
	case <-t.C:
		return func() {}, false
	case <-ctx.Done():
		return func() {}, false
	}
}

// launch starts rn, or parks it as "queued" until a slot frees. The slot,
// when concurrency is bounded, is held for exactly the execRun call.
func (h *httpServer) launch(ctx context.Context, rn *asyncRun, resume bool) {
	if release, ok := h.tryAcquireSlot(); ok {
		rn.mu.Lock()
		rn.state, rn.updated = RunStateWorking, time.Now()
		rn.mu.Unlock()
		h.publishRunStatus(rn)
		go func() {
			defer release()
			h.execRun(ctx, rn, resume)
		}()
		return
	}
	rn.mu.Lock()
	rn.state, rn.updated = RunStateQueued, time.Now()
	rn.mu.Unlock()
	h.publishRunStatus(rn)
	h.srv.logEvent(slog.LevelInfo, "run queued",
		"runId", rn.id, "owner", string(rn.owner), "tool", rn.tool.Name)
	go h.waitAndRun(ctx, rn, resume)
}

// waitAndRun blocks for a slot on behalf of a queued run, then runs it — or
// gives up if the run was cancelled (run/cancel or shutdown) while it waited.
// execRun closes rn.done on its own; the give-up paths must close it here.
func (h *httpServer) waitAndRun(ctx context.Context, rn *asyncRun, resume bool) {
	release, ok := h.acquireSlot(ctx)
	if !ok {
		rn.mu.Lock()
		if rn.state == RunStateQueued {
			rn.state, rn.updated = RunStateCanceled, time.Now()
		}
		rn.mu.Unlock()
		close(rn.done)
		return
	}
	rn.mu.Lock()
	if rn.state != RunStateQueued { // cancelled between the select and here
		rn.mu.Unlock()
		release()
		close(rn.done)
		return
	}
	rn.state, rn.updated = RunStateWorking, time.Now()
	rn.mu.Unlock()
	h.publishRunStatus(rn)
	defer release()
	h.execRun(ctx, rn, resume)
}

// publishRunStatus writes rn's current status to the shared checkpoint store so
// another replica's run/status can see progress it did not itself produce. A
// no-op unless the store is shared between replicas.
func (h *httpServer) publishRunStatus(rn *asyncRun) {
	if !h.cps.Shared() {
		return
	}
	rn.mu.Lock()
	rec := RunStatusRec{
		Tool: rn.tool.Name, State: rn.state, Holder: rn.holder, Step: rn.step,
		StepIndex: rn.stepIndex, StepTotal: rn.stepTotal,
		Reached:   append([]string(nil), rn.reached...),
		Resumable: rn.resumable, Error: auth.Redact(rn.errMsg),
		StartedAt: rn.started, UpdatedAt: rn.updated,
	}
	// Final vars (set only once the run reaches a terminal state) travel with
	// the record so a replica that reconstructs the run reports the same
	// result — redacted, so resolved credentials never reach the shared store.
	if rn.vars != nil {
		rec.Vars = runtime.RedactVars(rn.vars)
	}
	rn.mu.Unlock()
	_ = h.cps.WriteStatus(rn.id, rec)
}

// watchRemoteCancel polls the shared store for a distributed run/cancel while rn
// executes on this replica, and cancels its context when it sees one — so a
// cancel issued at another replica reaches this goroutine. Stops when stop is
// closed. A no-op unless the store is shared.
func (h *httpServer) watchRemoteCancel(rn *asyncRun, stop <-chan struct{}) {
	if !h.cps.Shared() {
		return
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if !h.cps.CancelRequested(rn.id) {
				continue
			}
			rn.mu.Lock()
			if rn.state == RunStateWorking {
				rn.state, rn.updated = RunStateCanceled, time.Now()
			}
			rn.mu.Unlock()
			rn.cancel()
			return
		}
	}
}

// hbAction is what one heartbeat tick decides to do.
type hbAction int

const (
	hbContinue  hbAction = iota // keep going (renew failed transiently, lease still has time)
	hbRefreshed                 // lease renewed, reset the safety clock
	hbCancel                    // the lease is lost or about to expire — stop working on this run
)

// heartbeatDecision maps a renew result to an action. A store error is only
// tolerated while the lease still has a safe margin left (renewTTL minus one
// heartbeat interval): past that a takeover could already be starting elsewhere,
// so the worker must stop rather than risk two writers.
func heartbeatDecision(stillMine bool, err error, sinceLastRenew time.Duration) hbAction {
	switch {
	case err == nil && stillMine:
		return hbRefreshed
	case err == nil && !stillMine:
		return hbCancel
	default:
		if sinceLastRenew >= runLockTTL-runLockHeartbeat {
			return hbCancel
		}
		return hbContinue
	}
}

// heartbeatLock renews lease while the run executes. It cancels the run when the
// lease is lost (another replica took it over) or when renews have failed long
// enough that the lease is about to expire — stopping before the safe window
// closes, not after N failures. Each renew is bounded by renewBudget so a wedged
// store cannot stall the loop, and an independent watchdog cancels the run if no
// renew succeeds within the safe window even if a renew call never returns.
// Stops when stop is closed (execRun returning).
func (h *httpServer) heartbeatLock(rn *asyncRun, lease leaseHandle, stop <-chan struct{}) {
	if h.lock == nil {
		return
	}
	t := time.NewTicker(runLockHeartbeat)
	defer t.Stop()
	safeWindow := runLockTTL - runLockHeartbeat
	watchdog := time.NewTimer(safeWindow)
	defer watchdog.Stop()
	resetWatchdog := func() {
		if !watchdog.Stop() {
			select {
			case <-watchdog.C:
			default:
			}
		}
		watchdog.Reset(safeWindow)
	}
	lastRenew := time.Now()
	for {
		select {
		case <-stop:
			return
		case <-watchdog.C:
			h.srv.logEvent(slog.LevelError,
				"run lock watchdog fired — no successful renew within the safe window; cancelling to avoid two writers",
				"runId", rn.id, "sinceLastRenew", time.Since(lastRenew).String())
			rn.cancel()
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), renewBudget)
			stillMine, err := h.lock.renew(ctx, lease)
			cancel()
			switch heartbeatDecision(stillMine, err, time.Since(lastRenew)) {
			case hbRefreshed:
				lastRenew = time.Now()
				resetWatchdog()
			case hbContinue:
				h.srv.logEvent(slog.LevelWarn, "run lock renew failed — retrying before the lease expires",
					"runId", rn.id, "err", err.Error(), "sinceLastRenew", time.Since(lastRenew).String())
			case hbCancel:
				if err != nil {
					h.srv.logEvent(slog.LevelError,
						"run lock renew failing and the lease is about to expire — cancelling to avoid two writers",
						"runId", rn.id, "err", err.Error(), "sinceLastRenew", time.Since(lastRenew).String())
				} else {
					h.srv.logEvent(slog.LevelWarn, "run lock lost — another replica took over; cancelling",
						"runId", rn.id)
				}
				rn.cancel()
				return
			}
		}
	}
}

// refreshRemote re-reads a reconstructed run's published status so repeated
// run/status polls reflect progress made on the owning replica. A local
// run/cancel already recorded on this replica wins over a stale "working".
func (h *httpServer) refreshRemote(rn *asyncRun) {
	if rn == nil || !rn.remote {
		return
	}
	rec, ok := h.cps.ReadStatus(rn.id)
	if !ok {
		return
	}
	rn.mu.Lock()
	if rn.state != RunStateCanceled {
		rn.state = rec.State
	}
	rn.step, rn.stepIndex, rn.stepTotal = rec.Step, rec.StepIndex, rec.StepTotal
	if len(rec.Reached) > 0 {
		rn.reached = append([]string(nil), rec.Reached...)
	}
	rn.resumable = rec.Resumable
	rn.errMsg = rec.Error
	if rec.Vars != nil {
		rn.vars = rec.Vars // redacted at publish time
	}
	if !rec.UpdatedAt.IsZero() {
		rn.updated = rec.UpdatedAt
	}
	rn.mu.Unlock()

	h.markIfLeaseExpired(rn)
}

// markIfLeaseExpired downgrades a reconstructed run that the shared status
// says is "working" but whose run lock is gone or stale: the worker that was
// driving it is presumed dead, so the run is reported failed-but-resumable
// (a takeover is an explicit run/resume). A no-op without a run lock.
func (h *httpServer) markIfLeaseExpired(rn *asyncRun) {
	if h.lock == nil || rn == nil || !rn.remote {
		return
	}
	rn.mu.Lock()
	working := rn.state == RunStateWorking
	rn.mu.Unlock()
	if !working {
		return
	}
	// Only downgrade when we can positively read that the lease is gone/stale.
	// A store error (lockUnknown) is not proof the worker died.
	if st, _, err := h.lock.peek(context.Background(), rn.id); err != nil || st == lockFresh || st == lockUnknown {
		return
	}
	rn.mu.Lock()
	rn.state = RunStateFailed
	rn.errMsg = "worker lease expired — resumable on another replica"
	rn.resumable = h.cps.Exists(rn.id)
	rn.updated = time.Now()
	rn.mu.Unlock()
}

// handleRun dispatches this server's run/* async-execution extension. The
// caller has already enforced the protocol context (as for tools/*).
func (h *httpServer) handleRun(sess *session, msg rpcMsg) *rpcMsg {
	switch msg.Method {
	case "run/start":
		return h.runStart(sess, msg)
	case "run/status":
		return h.runStatus(sess, msg)
	case "run/resume":
		return h.runResume(sess, msg)
	case "run/cancel":
		return h.runCancel(sess, msg)
	case "run/list":
		return h.runList(sess, msg)
	case "run/logs":
		return h.runLogs(sess, msg)
	default:
		return errMsg(msg.ID, -32601, "method not found: "+msg.Method)
	}
}

// maxRunInputBytes caps the JSON size of run/start (and run/resume) arguments.
// Durable intake persists these into the run record, read back by whichever
// replica claims the run; an oversized payload belongs in a blob store, not a
// state row. Spill-to-blob is Etapa 2 — until then, over the limit is refused.
const maxRunInputBytes = 256 << 10

// checkRunInputs enforces what run/start and run/resume arguments must satisfy
// to travel with a durable run: JSON-serialisable (no closures/handles — they
// cannot survive a checkpoint or a claim by another replica) and within
// maxRunInputBytes. Over the MCP transport arguments arrive as JSON already, so
// the serialisability check is defence for the in-process caller; the size
// bound applies to every path.
func checkRunInputs(args map[string]any) error {
	if len(args) == 0 {
		return nil
	}
	if err := runtime.CheckpointableVars(args); err != nil {
		return err
	}
	if b, err := json.Marshal(args); err == nil && len(b) > maxRunInputBytes {
		return fmt.Errorf("run inputs are %d bytes, over the %d-byte limit — pass large data by reference", len(b), maxRunInputBytes)
	}
	return nil
}

// runStart begins a workflow in the background and replies immediately with
// its runId and initial ("working") status. params mirror tools/call:
// {name, arguments}.
func (h *httpServer) runStart(sess *session, msg rpcMsg) *rpcMsg {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return errMsg(msg.ID, -32602, "invalid params: "+err.Error())
		}
	}
	w, ok := h.srv.tools[p.Name]
	if !ok {
		return errMsg(msg.ID, -32602, fmt.Sprintf("unknown tool %q", p.Name))
	}
	// Enforce the advertised inputSchema before a run is registered, a slot
	// taken, or a goroutine launched: a malformed run/start fails fast with
	// -32602 naming the field, not as a late state:"failed" on the first step.
	if err := w.Pipeline.ValidateInputs(p.Arguments); err != nil {
		return errMsg(msg.ID, -32602, err.Error())
	}
	// Durable-intake contract (Etapa 0): a run's inputs must be JSON-serialisable
	// and bounded, because once durable intake lands they are persisted with the
	// run record and read back by whichever replica claims it. The runId this
	// call returns is durable from here on — there is no separate claim_id.
	if err := checkRunInputs(p.Arguments); err != nil {
		return errMsg(msg.ID, -32602, err.Error())
	}

	// The run outlives this request, so its context descends from runsCtx
	// (the drain-aware child of the server lifetime), not r.Context().
	// run/cancel, the drain deadline, and shutdown all stop it.
	ctx, cancel := context.WithCancel(h.runsCtx)
	rn := &asyncRun{
		id:        runtime.NewSessionID(),
		owner:     h.ownerOf(sess),
		principal: sess.principal,
		tool:      w,
		args:      p.Arguments,
		started:   time.Now(),
		updated:   time.Now(),
		cancel:    cancel,
		done:      make(chan struct{}),
		logs:      newRingLog(),
		state:     RunStateQueued, // launch sets the authoritative state synchronously
	}
	h.runs.Put(rn)

	// Durable intake (Etapa 1): when a cas store is configured, run/start does
	// not launch. It persists the intake record and a `pending` status, then
	// returns — the per-replica claim loop takes it from there (on this replica
	// or another), so an accepted run survives a crash before its first step.
	// Without a cas store the historical in-memory path runs unchanged.
	if h.claimKV != nil {
		if err := h.writeIntake(ctx, rn); err != nil {
			// Persisting the run failed: fall back rather than drop it.
			h.srv.logEvent(slog.LevelError, "durable intake write failed — launching in-memory only",
				"runId", rn.id, "err", err.Error())
			if sess.principal != "" {
				_ = h.cps.WriteOwner(rn.id, rn.owner)
			}
			h.launch(ctx, rn, false)
			return h.srv.replyResult(sess, msg.ID, h.runView(rn))
		}
		rn.mu.Lock()
		rn.state, rn.updated = RunStatePending, time.Now()
		rn.mu.Unlock()
		_ = h.cps.WriteOwner(rn.id, rn.owner)
		h.publishRunStatus(rn)
		h.nudgeClaim()
		return h.srv.replyResult(sess, msg.ID, h.runView(rn))
	}

	// Persist the owner only for a verified principal: a session-hash owner
	// (no verifier) can't survive a restart anyway — each process mints fresh
	// session ids — so cross-restart reclaim stays as in Phase 0 there.
	if sess.principal != "" {
		_ = h.cps.WriteOwner(rn.id, rn.owner)
	}
	h.launch(ctx, rn, false)
	return h.srv.replyResult(sess, msg.ID, h.runView(rn))
}

// runResume relaunches a stopped run from its checkpoint. params:
// {runId, arguments?} — arguments are merged over the original inputs (the
// place to pass an approval decision the gate step reads).
func (h *httpServer) runResume(sess *session, msg rpcMsg) *rpcMsg {
	var p struct {
		RunID     string         `json:"runId"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return errMsg(msg.ID, -32602, "invalid params: "+err.Error())
		}
	}
	rn := h.ownedRun(p.RunID, sess)
	if rn == nil {
		return errMsg(msg.ID, -32602, fmt.Sprintf("unknown runId %q", p.RunID))
	}
	// Merged resume arguments are persisted with the run just like run/start's,
	// so they carry the same serialisable-and-bounded contract.
	if p.Arguments != nil {
		if err := checkRunInputs(p.Arguments); err != nil {
			return errMsg(msg.ID, -32602, err.Error())
		}
	}
	// A run suspended by pause(...) is always resumable — it wrote its own
	// checkpoint. Otherwise the workflow must not have opted out of the
	// default per-step checkpointing with `checkpoint: { enabled: false }`.
	rn.mu.Lock()
	paused := rn.state == RunStatePaused
	rn.mu.Unlock()
	if !paused && !rn.tool.Pipeline.Checkpoint.Enabled {
		return errMsg(msg.ID, -32602, fmt.Sprintf("run %q's workflow declares checkpoint: { enabled: false } — nothing to resume", p.RunID))
	}
	if !h.cps.Exists(rn.id) {
		return errMsg(msg.ID, -32602, fmt.Sprintf("run %q has no checkpoint on disk to resume from", p.RunID))
	}

	// A fresh run lock held by another replica means the run is genuinely
	// executing there — refuse. An absent/expired lock means the worker is
	// gone, so a "working" run is actually resumable (takeover). execRun's own
	// acquire is the authoritative gate; this is the fast, clear error.
	lockAlive := false
	if h.lock != nil {
		st, holder, err := h.lock.peek(context.Background(), rn.id)
		if err != nil {
			// Cannot read the lease: refuse rather than risk relaunching a run
			// that is genuinely executing elsewhere.
			return errMsg(msg.ID, -32603, fmt.Sprintf("cannot verify run %q execution lease: %v", p.RunID, err))
		}
		if st == lockFresh {
			if holder != h.replicaID {
				return errMsg(msg.ID, -32602, fmt.Sprintf("run %q is executing on another replica (%s)", p.RunID, holder))
			}
			lockAlive = true
		}
	}

	rn.mu.Lock()
	if rn.state == RunStateWorking && (h.lock == nil || lockAlive) {
		rn.mu.Unlock()
		return errMsg(msg.ID, -32602, fmt.Sprintf("run %q is still working", p.RunID))
	}
	if p.Arguments != nil {
		if rn.args == nil {
			rn.args = map[string]any{}
		}
		for k, v := range p.Arguments {
			rn.args[k] = v
		}
	}
	ctx, cancel := context.WithCancel(h.runsCtx)
	rn.errMsg, rn.updated = "", time.Now()
	rn.principal = sess.principal // context.principal for this leg = the resumer
	rn.cancel, rn.done = cancel, make(chan struct{})
	rn.mu.Unlock()

	h.runs.Put(rn)
	if sess.principal != "" { // (re)bind — see runStart
		_ = h.cps.WriteOwner(rn.id, rn.owner)
	}

	h.launch(ctx, rn, true)
	return h.srv.replyResult(sess, msg.ID, h.runView(rn))
}

// execRun drives one asyncRun to a terminal state.
func (h *httpServer) execRun(ctx context.Context, rn *asyncRun, resume bool) {
	defer close(rn.done)
	defer rn.cancel()

	// Cross-replica run lock: exactly one replica may drive a given runId. A
	// confirmed lease is a hard precondition — if the store cannot be reached
	// to take it, this run does not start (it cannot rule out another writer).
	var lease leaseHandle // set when h.lock != nil; used to fence checkpoint writes
	if h.lock != nil {
		var held bool
		var holder string
		var err error
		lease, held, holder, err = h.lock.acquire(ctx, rn.id)
		switch {
		case err != nil:
			rn.mu.Lock()
			rn.state = RunStateFailed
			rn.errMsg = fmt.Sprintf("could not acquire run execution lease: %v", err)
			rn.updated = time.Now()
			rn.resumable = h.cps.Exists(rn.id)
			rn.mu.Unlock()
			h.srv.logEvent(slog.LevelError, "run lock acquire failed — refusing to run",
				"runId", rn.id, "err", err.Error())
			h.publishRunStatus(rn)
			return
		case !held:
			rn.mu.Lock()
			rn.state = RunStateFailed
			rn.errMsg = fmt.Sprintf("run is executing on another replica (%s)", holder)
			rn.updated = time.Now()
			rn.resumable = h.cps.Exists(rn.id)
			rn.mu.Unlock()
			h.srv.logEvent(slog.LevelInfo, "run lock held elsewhere — refusing to run",
				"runId", rn.id, "holder", holder)
			h.publishRunStatus(rn)
			return
		}
		stopHB := make(chan struct{})
		defer close(stopHB)
		go h.heartbeatLock(rn, lease, stopHB)
		defer func() { _ = h.lock.release(context.WithoutCancel(ctx), lease) }()
	}

	// A cancel issued at another replica lands as a flag in the shared store;
	// this goroutine is what turns it into a ctx cancel here.
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go h.watchRemoteCancel(rn, stopWatch)

	start := time.Now()
	w := rn.tool
	h.srv.logEvent(slog.LevelInfo, "run started",
		"runId", rn.id, "owner", string(rn.owner), "tool", w.Name, "resume", resume)
	h.publishRunStatus(rn)

	var stateStore runtime.StateStore
	if h.store != nil {
		// Fence checkpoint writes on the execution lease: a replica that stalled
		// past its lease and was taken over fails its checkpoint writes rather
		// than corrupting the state the successor now drives.
		var fence func() error
		if h.lock != nil {
			fence = func() error {
				ok, ferr := h.lock.ownsLease(context.Background(), lease)
				if ferr != nil {
					return ferr // fail closed on a store error
				}
				if !ok {
					return ErrLeaseLost
				}
				return nil
			}
		}
		stateStore = newExtStateStore(h.store, rn.id, fence) // checkpoints go to the extension, not disk
	}
	res, runErr := execsvc.Run(execsvc.Request{
		Context:    ctx,
		Program:    w.Program,
		File:       w.File,
		Workflow:   w.Name,
		Inputs:     rn.args,
		BaseDir:    h.cps.BaseDir(),
		Session:    rn.id,
		Resume:     resume,
		Principal:  rn.principal,
		StateStore: stateStore,
		// Tee step/log() output to this run's own bounded buffer (for
		// run/logs) and to the shared diagnostics sink (stderr / kubectl logs).
		Out: io.MultiWriter(rn.logs, h.srv.logw),
		OnStep: func(step string, idx, total int) {
			rn.mu.Lock()
			rn.step, rn.stepIndex, rn.stepTotal = step, idx, total
			rn.reached = append(rn.reached, step)
			rn.updated = time.Now()
			rn.mu.Unlock()
			h.publishRunStatus(rn)
			// Step boundary: honour a distributed cancel even if the 1s
			// watcher tick has not landed yet.
			if h.cps.Shared() && h.cps.CancelRequested(rn.id) {
				rn.mu.Lock()
				if rn.state == RunStateWorking {
					rn.state, rn.updated = RunStateCanceled, time.Now()
				}
				rn.mu.Unlock()
				rn.cancel()
			}
		},
	})

	rn.mu.Lock()
	rn.updated = time.Now()
	if rn.state != RunStateCanceled {
		switch {
		case runErr != nil && ctx.Err() != nil:
			rn.state, rn.errMsg = RunStateCanceled, runErr.Error()
		case runErr != nil:
			rn.state, rn.errMsg = RunStateFailed, runErr.Error()
		case res != nil && res.Paused:
			// A step called pause(...): the run is suspended for a
			// human-in-the-loop hand-off. Not completed (its checkpoint and
			// state must survive for run/resume), not failed. The reason rides
			// in errMsg the same way a failure message does.
			rn.state = RunStatePaused
			rn.errMsg = pauseReasonText(res.PauseReason)
			rn.vars = res.Vars
			if all := append(append([]string{}, res.Skipped...), res.Executed...); len(all) > 0 {
				rn.reached = all
			}
		default:
			rn.state = RunStateCompleted
			if res != nil {
				rn.vars = res.Vars
				// Authoritative step list: what a resume skipped over, then
				// what this leg executed.
				if all := append(append([]string{}, res.Skipped...), res.Executed...); len(all) > 0 {
					rn.reached = all
				}
			}
		}
	}
	// A paused run is always resumable — pause(...) writes a checkpoint
	// unconditionally, no `checkpoint {}` block required. Otherwise a run is
	// resumable only with a per-step checkpoint on disk.
	if rn.state == RunStatePaused {
		rn.resumable = h.cps.Exists(rn.id)
	} else {
		rn.resumable = rn.state != RunStateCompleted && w.Pipeline.Checkpoint.Enabled && h.cps.Exists(rn.id)
	}
	terminal := rn.state
	steps := len(rn.reached)
	rn.mu.Unlock()

	// No more output can arrive for a run that has stopped for good, so run/logs
	// can stop holding back the secret-length tail it keeps while writes are
	// still possible. A paused run may yet resume and write more, so leave it.
	if terminal != RunStatePaused {
		rn.logs.Seal()
	}

	dur := time.Since(start)
	h.metrics.ObserveRun(string(terminal), dur)
	h.srv.logEvent(slog.LevelInfo, "run "+string(terminal),
		"runId", rn.id, "owner", string(rn.owner), "tool", w.Name,
		"durationMs", dur.Milliseconds(), "steps", steps)

	// Publish the terminal state (with final vars) so another replica's
	// run/status stops seeing "working" and reports the same result. A clean
	// finish clears its own per-step checkpoint (runtime does that), but the
	// status record is kept for a retention window — sweepRuns removes it from
	// the shared store once it is older than sessionTTL, the same bound the
	// in-memory registry uses. Removing it here made the same runId answer
	// "completed" on the replica that ran it and "unknown" on any other.
	h.publishRunStatus(rn)
}

func (h *httpServer) runStatus(sess *session, msg rpcMsg) *rpcMsg {
	id := runIDParam(msg.Params)
	rn := h.ownedRun(id, sess)
	if rn == nil {
		return errMsg(msg.ID, -32602, fmt.Sprintf("unknown runId %q", id))
	}
	h.adoptIfClaimedElsewhere(rn) // this replica accepted it, another claimed it
	h.refreshRemote(rn)           // a reconstructed run advances on the owning replica
	return h.srv.replyResult(sess, msg.ID, h.runView(rn))
}

// adoptIfClaimedElsewhere turns a locally-accepted run that another replica has
// claimed from durable intake into a remote view, so subsequent polls track it
// through the shared status record and run/cancel signals through the store.
func (h *httpServer) adoptIfClaimedElsewhere(rn *asyncRun) {
	if h.claimKV == nil || rn == nil {
		return
	}
	rn.mu.Lock()
	skip := rn.remote || !rn.state.IsPreExecution()
	rn.mu.Unlock()
	if skip {
		return
	}
	rec, ok := h.cps.ReadStatus(rn.id)
	if !ok || rec.Holder == "" || rec.Holder == h.replicaID {
		return
	}
	rn.mu.Lock()
	rn.remote = true
	rn.mu.Unlock()
}

// runLogs returns this run's retained step/log() output from a byte cursor.
// params: {runId, since?}. reply: {text, nextSince, dropped?}. Poll with the
// previous nextSince to stream. Owner-gated like run/status.
func (h *httpServer) runLogs(sess *session, msg rpcMsg) *rpcMsg {
	var p struct {
		RunID string `json:"runId"`
		Since int64  `json:"since"`
	}
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return errMsg(msg.ID, -32602, "invalid params: "+err.Error())
		}
	}
	rn := h.ownedRun(p.RunID, sess)
	if rn == nil {
		return errMsg(msg.ID, -32602, fmt.Sprintf("unknown runId %q", p.RunID))
	}
	text, next, dropped := rn.logs.read(p.Since)
	out := map[string]any{"runId": rn.id, "text": text, "nextSince": next}
	if dropped {
		out["dropped"] = true
	}
	return h.srv.replyResult(sess, msg.ID, out)
}

func (h *httpServer) runCancel(sess *session, msg rpcMsg) *rpcMsg {
	id := runIDParam(msg.Params)
	rn := h.ownedRun(id, sess)
	if rn == nil {
		return errMsg(msg.ID, -32602, fmt.Sprintf("unknown runId %q", id))
	}
	if rn.remote {
		// The goroutine runs on another replica — signal it through the shared
		// store; watchRemoteCancel / the step boundary there stop it. (Still
		// gated by ownership above; this is coordination, not a new grant.)
		_ = h.cps.RequestCancel(rn.id)
	} else {
		rn.cancel() // wakes a queued run's waitAndRun, which finishes the cancel
	}
	rn.mu.Lock()
	// working or any pre-execution state (queued, or durable pending/claimed) →
	// canceled. A pre-execution run has no goroutine to stop and no external
	// effect yet; run/cancel just records the state.
	changed := false
	if rn.state == RunStateWorking || rn.state.IsPreExecution() {
		rn.state, rn.updated = RunStateCanceled, time.Now()
		changed = true
	}
	rn.mu.Unlock()
	// With durable intake, persist the terminal state so the claim loop stops
	// selecting this pending run, and signal any replica that may have claimed
	// it in the same instant.
	if changed && h.claimKV != nil {
		h.publishRunStatus(rn)
		_ = h.cps.RequestCancel(rn.id)
	}
	return h.srv.replyResult(sess, msg.ID, h.runView(rn))
}

func (h *httpServer) runList(sess *session, msg rpcMsg) *rpcMsg {
	owner := h.ownerOf(sess)
	runs := h.runs.List()
	sort.Slice(runs, func(i, j int) bool { return runs[i].id < runs[j].id })
	views := make([]map[string]any, 0, len(runs))
	for _, rn := range runs {
		if rn.owner == owner {
			views = append(views, h.runView(rn))
		}
	}
	return h.srv.replyResult(sess, msg.ID, map[string]any{"runs": views})
}

// ownerOf is the Owner a run started or resumed by sess belongs to: the
// verified principal when a TokenVerifier produced one (Phase 2), else the
// Phase-0 per-session hash so a plain --token / no-verifier deployment is
// unchanged.
func (h *httpServer) ownerOf(sess *session) Owner {
	if sess.principal != "" {
		return ownerFor(sess.principal)
	}
	return ownerFromSession(sess.id)
}

// ownedRun resolves a runId for the calling session: an in-memory run, or
// one reconstructed from disk (claimed for this caller — a run whose owner
// session is gone after a restart). Returns nil when the run does not exist
// or belongs to another caller — callers surface both as "unknown runId" so
// the method is not an existence oracle.
func (h *httpServer) ownedRun(id string, sess *session) *asyncRun {
	owner := h.ownerOf(sess)
	rn, ok := h.runs.Get(id)
	if !ok {
		rn = h.reconstructRun(id, owner)
	}
	if rn == nil || rn.owner != owner {
		return nil
	}
	return rn
}

// reconstructRun rebuilds an asyncRun for a run that is not in this replica's
// registry: it was swept, or a restart lost it, or it is executing on another
// replica right now. It works from a resumable checkpoint, or — when there is
// none yet (a run without per_step still working elsewhere) — from the live
// status record the owning replica publishes each step boundary.
//
// It returns nil when there is nothing to rebuild from, or when a persisted
// owner (CheckpointStore.WriteOwner) does not match ownerK. When no owner was
// persisted (a session-hash owner, or an anonymous "" one) the run is claimed
// for ownerK, the historical behaviour.
func (h *httpServer) reconstructRun(id string, ownerK Owner) *asyncRun {
	if id == "" {
		return nil
	}
	if persisted, ok := h.cps.ReadOwner(id); ok && persisted != ownerK {
		return nil
	}

	if cp, ok := h.cps.Load(id); ok {
		w, ok := h.srv.tools[cp.Pipeline]
		if !ok {
			return nil
		}
		rn := &asyncRun{
			id: id, owner: ownerK, tool: w, args: map[string]any{},
			started: cp.SavedAt, updated: cp.SavedAt,
			cancel: func() {}, done: make(chan struct{}),
			logs:      newRingLog(), // empty: this run's output was in a prior process
			remote:    true,
			state:     RunStateFailed,
			step:      cp.NextStep,
			stepTotal: len(w.Pipeline.Steps),
			reached:   append([]string(nil), cp.CompletedSteps...),
			resumable: true,
		}
		// A newer live status (e.g. it is working again on another replica)
		// overrides the checkpoint-derived snapshot.
		h.runs.Put(rn)
		h.refreshRemote(rn)
		return rn
	}

	// No checkpoint — try the live status record.
	rec, ok := h.cps.ReadStatus(id)
	if !ok {
		return nil
	}
	w, ok := h.srv.tools[rec.Tool]
	if !ok {
		return nil
	}
	rn := &asyncRun{
		id: id, owner: ownerK, tool: w, args: map[string]any{},
		started: rec.StartedAt, updated: rec.UpdatedAt,
		cancel: func() {}, done: make(chan struct{}),
		logs:      newRingLog(),
		remote:    true,
		state:     rec.State,
		step:      rec.Step,
		stepIndex: rec.StepIndex,
		stepTotal: rec.StepTotal,
		reached:   append([]string(nil), rec.Reached...),
		resumable: rec.Resumable,
		errMsg:    rec.Error,
		vars:      rec.Vars, // already redacted at publish time
	}
	if rn.state == "" {
		rn.state = RunStateWorking
	}
	h.runs.Put(rn)
	h.markIfLeaseExpired(rn)
	return rn
}

// runView renders an asyncRun as the JSON status object a run/* reply carries.
func (h *httpServer) runView(rn *asyncRun) map[string]any {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	v := map[string]any{
		"runId":     rn.id,
		"tool":      rn.tool.Name,
		"state":     rn.state,
		"startedAt": rn.started.UTC().Format(time.RFC3339),
	}
	if rn.step != "" {
		v["step"] = rn.step
		v["stepIndex"] = rn.stepIndex
		v["stepTotal"] = rn.stepTotal
	}
	if len(rn.reached) > 0 {
		v["reached"] = append([]string(nil), rn.reached...)
	}
	if rn.resumable {
		v["resumable"] = true
	}
	if (rn.state == RunStateCompleted || rn.state == RunStatePaused) && rn.vars != nil {
		// Redacted here too, so the replica that ran the workflow and a replica
		// that reconstructs it from the shared status record return the same
		// vars, and no resolved credential is ever surfaced.
		v["vars"] = runtime.RedactVars(rn.vars)
	}
	if rn.errMsg != "" {
		// A failure/pause message can quote a resolved credential; mask it on
		// the same policy as vars above.
		if rn.state == RunStatePaused {
			v["reason"] = auth.Redact(rn.errMsg)
		} else {
			v["error"] = auth.Redact(rn.errMsg)
		}
	}
	return v
}

// sweepRuns is opportunistic registry housekeeping, called on `initialize`.
// A completed run older than sessionTTL is dropped and its (already-cleared)
// state dir removed. A stopped run with no checkpoint on disk can never be
// resumed and is dropped too. A *resumable* run (failed/canceled with a
// checkpoint) is kept in the registry for the whole process lifetime so its
// owner binding keeps holding — on-disk state is GC'd by runtime.PruneExpired
// once its TTL passes.
//
// When the store is shared between replicas it also retires terminal-run
// status records the shared store keeps: execRun no longer deletes a completed
// run's record on the spot (that made the same runId resolve on the replica
// that ran it and 404 everywhere else), so this is what bounds the store's
// growth — same sessionTTL window the in-memory registry uses.
func (h *httpServer) sweepRuns() {
	cut := time.Now().Add(-sessionTTL)
	for _, rn := range h.runs.List() {
		rn.mu.Lock()
		state := rn.state
		old := rn.updated.Before(cut)
		remote := rn.remote
		rn.mu.Unlock()
		switch {
		case state == RunStateCompleted && old:
			h.runs.Delete(rn.id)
			// A locally-run completion clears its whole state dir here. A run
			// reconstructed from another replica only drops this pod's copy —
			// retiring the shared record is the walk below's job, on one
			// consistent UpdatedAt clock.
			if !remote {
				_ = h.cps.Remove(rn.id)
			}
		case (state == RunStateFailed || state == RunStateCanceled) && !h.cps.Exists(rn.id):
			h.runs.Delete(rn.id)
		}
	}

	if !h.cps.Shared() {
		return
	}
	statuses, err := h.cps.ListStatuses()
	if err != nil {
		return
	}
	for id, rec := range statuses {
		if !rec.State.IsTerminal() {
			continue // pending / claimed / queued / working / paused — still live
		}
		if rec.UpdatedAt.IsZero() || !rec.UpdatedAt.Before(cut) {
			continue // inside the retention window (or undatable)
		}
		if h.cps.Exists(id) {
			continue // a resumable checkpoint is still there — leave it for run/resume
		}
		_ = h.cps.Remove(id)
	}
}

// cleanupRuns cancels every tracked run — called once on server shutdown.
// The state tree is removed only when we own a throwaway one; a caller-given
// --state-dir is left so runs can be resumed by a later process.
func (h *httpServer) cleanupRuns() {
	for _, rn := range h.runs.List() {
		rn.cancel()
		h.runs.Delete(rn.id)
	}
	_ = h.cps.Close()
}

// pauseReasonText renders a pause(...) reason value for the run status —
// a bare string as-is, anything else as compact JSON, nil as "paused".
func pauseReasonText(v any) string {
	switch t := v.(type) {
	case nil:
		return "paused"
	case string:
		return t
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return "paused"
	}
}

func runIDParam(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var p struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(params, &p)
	return p.RunID
}
