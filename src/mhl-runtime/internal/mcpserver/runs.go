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

	mu        sync.Mutex
	state     string // "queued" | "working" | "completed" | "failed" | "canceled" | "paused"
	step      string // last step reached
	stepIndex int    // 1-based position of step
	stepTotal int    // pipeline's declared step count
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
		rn.state, rn.updated = "working", time.Now()
		rn.mu.Unlock()
		h.publishRunStatus(rn)
		go func() {
			defer release()
			h.execRun(ctx, rn, resume)
		}()
		return
	}
	rn.mu.Lock()
	rn.state, rn.updated = "queued", time.Now()
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
		if rn.state == "queued" {
			rn.state, rn.updated = "canceled", time.Now()
		}
		rn.mu.Unlock()
		close(rn.done)
		return
	}
	rn.mu.Lock()
	if rn.state != "queued" { // cancelled between the select and here
		rn.mu.Unlock()
		release()
		close(rn.done)
		return
	}
	rn.state, rn.updated = "working", time.Now()
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
		Tool: rn.tool.Name, State: rn.state, Step: rn.step,
		StepIndex: rn.stepIndex, StepTotal: rn.stepTotal,
		Reached:   append([]string(nil), rn.reached...),
		Resumable: rn.resumable, Error: rn.errMsg,
		StartedAt: rn.started, UpdatedAt: rn.updated,
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
			if rn.state == "working" {
				rn.state, rn.updated = "canceled", time.Now()
			}
			rn.mu.Unlock()
			rn.cancel()
			return
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
	if rn.state != "canceled" {
		rn.state = rec.State
	}
	rn.step, rn.stepIndex, rn.stepTotal = rec.Step, rec.StepIndex, rec.StepTotal
	if len(rec.Reached) > 0 {
		rn.reached = append([]string(nil), rec.Reached...)
	}
	rn.resumable = rec.Resumable
	rn.errMsg = rec.Error
	if !rec.UpdatedAt.IsZero() {
		rn.updated = rec.UpdatedAt
	}
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
		state:     "queued", // launch sets the authoritative state synchronously
	}
	h.runs.Put(rn)
	// Persist the owner only for a verified principal: a session-hash owner
	// (no verifier) can't survive a restart anyway — each process mints fresh
	// session ids — so cross-restart reclaim stays as in Phase 0 there.
	if sess.principal != "" {
		_ = h.cps.WriteOwner(rn.id, rn.owner)
	}

	h.launch(ctx, rn, false)
	return h.srv.replyResult(sess, msg.ID, h.runViewFor(rn))
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
	// A run suspended by pause(...) is always resumable — it wrote its own
	// checkpoint. Otherwise the workflow must not have opted out of the
	// default per-step checkpointing with `checkpoint: { enabled: false }`.
	rn.mu.Lock()
	paused := rn.state == "paused"
	rn.mu.Unlock()
	if !paused && !rn.tool.Pipeline.Checkpoint.Enabled {
		return errMsg(msg.ID, -32602, fmt.Sprintf("run %q's workflow declares checkpoint: { enabled: false } — nothing to resume", p.RunID))
	}
	if !h.cps.Exists(rn.id) {
		return errMsg(msg.ID, -32602, fmt.Sprintf("run %q has no checkpoint on disk to resume from", p.RunID))
	}

	rn.mu.Lock()
	if rn.state == "working" {
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
	return h.srv.replyResult(sess, msg.ID, h.runViewFor(rn))
}

// execRun drives one asyncRun to a terminal state.
func (h *httpServer) execRun(ctx context.Context, rn *asyncRun, resume bool) {
	defer close(rn.done)
	defer rn.cancel()

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
		stateStore = newExtStateStore(h.store, rn.id) // checkpoints go to the extension, not disk
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
				if rn.state == "working" {
					rn.state, rn.updated = "canceled", time.Now()
				}
				rn.mu.Unlock()
				rn.cancel()
			}
		},
	})

	rn.mu.Lock()
	rn.updated = time.Now()
	if rn.state != "canceled" {
		switch {
		case runErr != nil && ctx.Err() != nil:
			rn.state, rn.errMsg = "canceled", runErr.Error()
		case runErr != nil:
			rn.state, rn.errMsg = "failed", runErr.Error()
		case res != nil && res.Paused:
			// A step called pause(...): the run is suspended for a
			// human-in-the-loop hand-off. Not completed (its checkpoint and
			// state must survive for run/resume), not failed. The reason rides
			// in errMsg the same way a failure message does.
			rn.state = "paused"
			rn.errMsg = pauseReasonText(res.PauseReason)
			rn.vars = res.Vars
			if all := append(append([]string{}, res.Skipped...), res.Executed...); len(all) > 0 {
				rn.reached = all
			}
		default:
			rn.state = "completed"
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
	if rn.state == "paused" {
		rn.resumable = h.cps.Exists(rn.id)
	} else {
		rn.resumable = rn.state != "completed" && w.Pipeline.Checkpoint.Enabled && h.cps.Exists(rn.id)
	}
	terminal := rn.state
	steps := len(rn.reached)
	rn.mu.Unlock()

	dur := time.Since(start)
	h.metrics.ObserveRun(terminal, dur)
	h.srv.logEvent(slog.LevelInfo, "run "+terminal,
		"runId", rn.id, "owner", string(rn.owner), "tool", w.Name,
		"durationMs", dur.Milliseconds(), "steps", steps)

	// Publish the terminal state so another replica's run/status stops seeing
	// "working". A clean finish then clears its own checkpoint (runtime does
	// that) and we drop the now-empty state dir — which also removes the status
	// / cancel markers. A stopped run keeps them for run/resume.
	h.publishRunStatus(rn)
	if terminal == "completed" {
		_ = h.cps.Remove(rn.id)
	}
}

func (h *httpServer) runStatus(sess *session, msg rpcMsg) *rpcMsg {
	id := runIDParam(msg.Params)
	rn := h.ownedRun(id, sess)
	if rn == nil {
		return errMsg(msg.ID, -32602, fmt.Sprintf("unknown runId %q", id))
	}
	h.refreshRemote(rn) // a reconstructed run advances on the owning replica
	return h.srv.replyResult(sess, msg.ID, h.runViewFor(rn))
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
	if rn.state == "working" || rn.state == "queued" {
		rn.state, rn.updated = "canceled", time.Now()
	}
	rn.mu.Unlock()
	return h.srv.replyResult(sess, msg.ID, h.runViewFor(rn))
}

func (h *httpServer) runList(sess *session, msg rpcMsg) *rpcMsg {
	owner := h.ownerOf(sess)
	runs := h.runs.List()
	sort.Slice(runs, func(i, j int) bool { return runs[i].id < runs[j].id })
	views := make([]map[string]any, 0, len(runs))
	for _, rn := range runs {
		if rn.owner == owner {
			views = append(views, h.runViewFor(rn))
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
			state:     "failed",
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
	}
	if rn.state == "" {
		rn.state = "working"
	}
	h.runs.Put(rn)
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
	if (rn.state == "completed" || rn.state == "paused") && rn.vars != nil {
		v["vars"] = rn.vars
	}
	if rn.errMsg != "" {
		if rn.state == "paused" {
			v["reason"] = rn.errMsg
		} else {
			v["error"] = rn.errMsg
		}
	}
	return v
}

// runViewFor is runView plus the fields that depend on other runs — kept out
// of runView so it never nests one run's lock inside another's.
func (h *httpServer) runViewFor(rn *asyncRun) map[string]any {
	v := h.runView(rn)
	if v["state"] == "queued" {
		v["queuePosition"] = h.queuePosition(rn)
	}
	return v
}

// queuePosition is how many other runs are queued ahead of rn (0 = next up).
// Call it without holding rn.mu.
func (h *httpServer) queuePosition(rn *asyncRun) int {
	pos := 0
	for _, other := range h.runs.List() {
		if other == rn {
			continue
		}
		other.mu.Lock()
		ahead := other.state == "queued" && other.started.Before(rn.started)
		other.mu.Unlock()
		if ahead {
			pos++
		}
	}
	return pos
}

// sweepRuns is opportunistic registry housekeeping, called on `initialize`.
// A completed run older than sessionTTL is dropped and its (already-cleared)
// state dir removed. A stopped run with no checkpoint on disk can never be
// resumed and is dropped too. A *resumable* run (failed/canceled with a
// checkpoint) is kept in the registry for the whole process lifetime so its
// owner binding keeps holding — on-disk state is GC'd by runtime.PruneExpired
// once its TTL passes.
func (h *httpServer) sweepRuns() {
	cut := time.Now().Add(-sessionTTL)
	for _, rn := range h.runs.List() {
		rn.mu.Lock()
		state := rn.state
		old := rn.updated.Before(cut)
		rn.mu.Unlock()
		switch {
		case state == "completed" && old:
			h.runs.Delete(rn.id)
			_ = h.cps.Remove(rn.id)
		case (state == "failed" || state == "canceled") && !h.cps.Exists(rn.id):
			h.runs.Delete(rn.id)
		}
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
