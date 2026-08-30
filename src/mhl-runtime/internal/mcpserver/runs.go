package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
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
// Every run is owned by the session that started it (see ownerFromSession);
// status, resume, cancel and list only act for a matching caller. A completed
// run is swept after sessionTTL; a resumable one is kept for the process
// lifetime so its owner binding holds, and its on-disk state is GC'd by
// runtime.PruneExpired. After a restart the owner session is gone, so the
// first caller to name the (unguessable) runId reclaims it.
type asyncRun struct {
	id string
	// owner is ownerFromSession(creating session id); run/status, run/resume,
	// run/cancel and run/list only act for a matching caller. Set once at
	// creation (or claimed on reconstruct after a restart) — immutable after.
	owner   Owner
	tool    execsvc.Workflow
	args    map[string]any
	started time.Time
	cancel  context.CancelFunc
	done    chan struct{}

	mu        sync.Mutex
	state     string // "working" | "completed" | "failed" | "canceled"
	step      string // last step reached
	stepIndex int    // 1-based position of step
	stepTotal int    // pipeline's declared step count
	reached   []string
	resumable bool // a checkpoint exists that run/resume can continue from
	vars      map[string]any
	errMsg    string
	updated   time.Time
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

	// The run outlives this request, so its context descends from the server
	// lifetime (baseCtx), not r.Context(). run/cancel and shutdown stop it.
	ctx, cancel := context.WithCancel(h.baseCtx)
	rn := &asyncRun{
		id:      runtime.NewSessionID(),
		owner:   ownerFromSession(sess.id),
		tool:    w,
		args:    p.Arguments,
		started: time.Now(),
		updated: time.Now(),
		cancel:  cancel,
		done:    make(chan struct{}),
		state:   "working",
	}
	h.runs.Put(rn)

	go h.execRun(ctx, rn, false)
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
	if !rn.tool.Pipeline.Checkpoint.Enabled {
		return errMsg(msg.ID, -32602, fmt.Sprintf("run %q's workflow declares no checkpoint { strategy: \"per_step\" } — nothing to resume", p.RunID))
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
	ctx, cancel := context.WithCancel(h.baseCtx)
	rn.state, rn.errMsg, rn.updated = "working", "", time.Now()
	rn.cancel, rn.done = cancel, make(chan struct{})
	rn.mu.Unlock()

	h.runs.Put(rn)

	go h.execRun(ctx, rn, true)
	return h.srv.replyResult(sess, msg.ID, h.runView(rn))
}

// execRun drives one asyncRun to a terminal state.
func (h *httpServer) execRun(ctx context.Context, rn *asyncRun, resume bool) {
	defer close(rn.done)
	defer rn.cancel()

	w := rn.tool
	res, runErr := execsvc.Run(execsvc.Request{
		Context:  ctx,
		Program:  w.Program,
		File:     w.File,
		Workflow: w.Name,
		Inputs:   rn.args,
		BaseDir:  h.cps.BaseDir(),
		Session:  rn.id,
		Resume:   resume,
		// A running tool's log()/step output goes to the diagnostics sink,
		// never to a protocol response.
		Out: h.srv.logw,
		OnStep: func(step string, idx, total int) {
			rn.mu.Lock()
			rn.step, rn.stepIndex, rn.stepTotal = step, idx, total
			rn.reached = append(rn.reached, step)
			rn.updated = time.Now()
			rn.mu.Unlock()
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
	rn.resumable = rn.state != "completed" && w.Pipeline.Checkpoint.Enabled && h.cps.Exists(rn.id)
	terminal := rn.state
	rn.mu.Unlock()

	// A clean finish clears its own checkpoint (runtime does that); drop the
	// now-empty state dir. A stopped run keeps it for run/resume.
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
	return h.srv.replyResult(sess, msg.ID, h.runView(rn))
}

func (h *httpServer) runCancel(sess *session, msg rpcMsg) *rpcMsg {
	id := runIDParam(msg.Params)
	rn := h.ownedRun(id, sess)
	if rn == nil {
		return errMsg(msg.ID, -32602, fmt.Sprintf("unknown runId %q", id))
	}
	rn.cancel()
	rn.mu.Lock()
	if rn.state == "working" {
		rn.state, rn.updated = "canceled", time.Now()
	}
	rn.mu.Unlock()
	return h.srv.replyResult(sess, msg.ID, h.runView(rn))
}

func (h *httpServer) runList(sess *session, msg rpcMsg) *rpcMsg {
	owner := ownerFromSession(sess.id)
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

// ownedRun resolves a runId for the calling session: an in-memory run, or
// one reconstructed from disk (claimed for this caller — a run whose owner
// session is gone after a restart). Returns nil when the run does not exist
// or belongs to another caller — callers surface both as "unknown runId" so
// the method is not an existence oracle.
func (h *httpServer) ownedRun(id string, sess *session) *asyncRun {
	owner := ownerFromSession(sess.id)
	rn, ok := h.runs.Get(id)
	if !ok {
		rn = h.reconstructRun(id, owner)
	}
	if rn == nil || rn.owner != owner {
		return nil
	}
	return rn
}

// reconstructRun rebuilds an asyncRun from on-disk checkpoint state for a run
// that is no longer in the registry (swept, or a fresh process after a
// restart) and claims it for ownerK. Returns nil when there is no resumable
// state.
func (h *httpServer) reconstructRun(id string, ownerK Owner) *asyncRun {
	if id == "" {
		return nil
	}
	cp, ok := h.cps.Load(id)
	if !ok {
		return nil
	}
	w, ok := h.srv.tools[cp.Pipeline]
	if !ok {
		return nil
	}
	rn := &asyncRun{
		id:        id,
		owner:     ownerK,
		tool:      w,
		args:      map[string]any{},
		started:   cp.SavedAt,
		updated:   cp.SavedAt,
		cancel:    func() {}, // replaced by runResume before any goroutine runs
		done:      make(chan struct{}),
		state:     "failed",
		step:      cp.NextStep,
		stepTotal: len(w.Pipeline.Steps),
		reached:   append([]string(nil), cp.CompletedSteps...),
		resumable: true,
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
	if rn.state == "completed" && rn.vars != nil {
		v["vars"] = rn.vars
	}
	if rn.errMsg != "" {
		v["error"] = rn.errMsg
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
