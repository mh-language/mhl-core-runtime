package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// claimPollInterval is the safety-net cadence of the claim loop; run/start also
// nudges it so an accepted run normally starts without waiting a full tick.
const claimPollInterval = time.Second

// reconcileStaleAfter is how long a non-terminal run may go without a status
// update before reconcile checks its lease. Two lock TTLs — a live run renews
// its lease well inside this, so the lease peek (authoritative) still gates the
// reclaim; this is only a cheap pre-filter to avoid peeking every run.
const reconcileStaleAfter = 2 * runLockTTL

// nudgeClaim wakes the claim loop now (non-blocking; a pending nudge is enough).
func (h *httpServer) nudgeClaim() {
	if h.claimNudge == nil {
		return
	}
	select {
	case h.claimNudge <- struct{}{}:
	default:
	}
}

// claimLoop runs on every replica while durable intake is active. It drains
// claimable runs whenever it is nudged (run/start) or the safety ticker fires,
// bounded by the concurrency semaphore. Started by buildHTTP; stops with runsCtx.
func (h *httpServer) claimLoop(ctx context.Context) {
	t := time.NewTicker(claimPollInterval)
	defer t.Stop()
	for {
		tick := false
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick = true
		case <-h.claimNudge:
		}
		if tick {
			// Reconcile only on the safety tick, not on a run/start nudge.
			h.reconcileRuns(ctx)
		}
		h.drainClaims(ctx)
	}
}

// reconcileRuns returns to `pending` any run in a non-terminal state
// (`claimed`, `working`, `paused`) that has gone stale and whose execution
// lease is positively gone — the replica that held it is presumed dead. The
// claim loop then re-claims it and, when a checkpoint exists, resumes from it
// (at-least-once: a step that had started may run again, absorbed by the
// destination's idempotency — see P1-8). A no-op without a run lock (a dead
// holder cannot be told from a slow one) or on a store-read error for a run.
func (h *httpServer) reconcileRuns(ctx context.Context) {
	if h.lock == nil || h.claimKV == nil {
		return
	}
	statuses, err := h.cps.ListStatuses()
	if err != nil {
		return
	}
	cut := time.Now().Add(-reconcileStaleAfter)
	for id, rec := range statuses {
		switch rec.State {
		case RunStateClaimed, RunStateWorking, RunStatePaused:
		default:
			continue // terminal or pending — nothing to reconcile
		}
		if rec.UpdatedAt.IsZero() || rec.UpdatedAt.After(cut) {
			continue
		}
		// The lease is the authoritative signal: reclaim only on a positive
		// read that none is held. A live worker renews well inside the window.
		st, _, perr := h.lock.peek(ctx, id)
		if perr != nil || st == lockFresh || st == lockUnknown {
			continue
		}
		raw, found, gerr := h.claimKV.Get(ctx, runKey(id, "status"))
		if gerr != nil || !found {
			continue
		}
		back := rec
		back.State = RunStatePending
		back.Holder = ""
		back.UpdatedAt = time.Now()
		if ok, _ := h.claimKV.CompareAndSwap(ctx, runKey(id, "status"), raw, back); ok {
			h.metrics.ObserveReconcile()
			h.srv.logEvent(slog.LevelWarn, "reconcile: run returned to pending — no live lease",
				"runId", id, "was", string(rec.State), "staleFor", time.Since(rec.UpdatedAt).String())
		}
	}
}

// drainClaims claims and launches pending runs until there is no free slot or
// nothing left to claim. Each claimed run holds one concurrency slot for the
// duration of its execution.
func (h *httpServer) drainClaims(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		release, ok := h.tryAcquireSlot()
		if !ok {
			return
		}
		claimStart := time.Now()
		id, _, claimed, err := claimNext(ctx, h.cps, h.claimKV, h.replicaID)
		if err != nil {
			h.srv.logEvent(slog.LevelWarn, "claim scan failed", "err", err.Error())
			release()
			return
		}
		if !claimed {
			release()
			return
		}
		h.metrics.ObserveClaim(time.Since(claimStart))
		rn := h.runForClaimed(id)
		if rn == nil {
			// No usable intake record (missing, or its tool is not loaded here).
			// Mark it failed so the scan does not spin on it forever.
			h.failClaimed(id, "claimed run has no usable intake record")
			release()
			continue
		}
		h.runClaimed(ctx, rn, release)
	}
}

// runForClaimed resolves the asyncRun for a just-claimed runId. It reuses the
// in-memory run only when this replica accepted it and it has not started here
// (a pre-execution state); otherwise — a run that already executed here and had
// its lease die, or a remote view — it rebuilds a fresh execution object from
// the durable intake record, so a reconcile → re-claim never starts a second
// goroutine on the same asyncRun. Returns nil when there is no intake record or
// its workflow is not loaded on this replica.
func (h *httpServer) runForClaimed(id string) *asyncRun {
	if rn, ok := h.runs.Get(id); ok {
		rn.mu.Lock()
		reuse := !rn.remote && rn.state.IsPreExecution()
		if reuse {
			rn.holder = h.replicaID
		}
		rn.mu.Unlock()
		if reuse {
			return rn
		}
	}
	rec, ok := h.readIntake(context.Background(), id)
	if !ok {
		return nil
	}
	w, ok := h.srv.tools[rec.Tool]
	if !ok {
		return nil
	}
	rn := &asyncRun{
		id:        id,
		owner:     rec.Owner,
		principal: rec.Principal,
		tool:      w,
		args:      rec.Args,
		started:   rec.CreatedAt,
		updated:   time.Now(),
		cancel:    func() {},
		done:      make(chan struct{}),
		logs:      newRingLog(),
		state:     RunStateClaimed,
		holder:    h.replicaID,
	}
	h.runs.Put(rn)
	return rn
}

// runClaimed drives a claimed run to execution, holding release (one slot) for
// the whole run. It gives the run a fresh cancel context descended from runsCtx.
func (h *httpServer) runClaimed(ctx context.Context, rn *asyncRun, release func()) {
	runCtx, cancel := context.WithCancel(h.runsCtx)
	rn.mu.Lock()
	if rn.cancel != nil {
		rn.cancel() // release the placeholder context from run/start, if any
	}
	rn.cancel = cancel
	if rn.done == nil {
		rn.done = make(chan struct{})
	}
	rn.state, rn.updated = RunStateWorking, time.Now()
	rn.mu.Unlock()
	h.publishRunStatus(rn)
	// Resume from a checkpoint when one exists (a run reconcile returned to
	// pending after it had already started); otherwise a fresh execution.
	resume := h.cps.Exists(rn.id)
	go func() {
		defer release()
		h.execRun(runCtx, rn, resume)
	}()
}

// failClaimed publishes a terminal failed status for a run that was claimed but
// cannot be run here, so the claim scan stops selecting it. sweepRuns retires
// the record on the usual retention window.
func (h *httpServer) failClaimed(id, reason string) {
	h.srv.logEvent(slog.LevelError, "claimed run cannot execute", "runId", id, "reason", reason)
	_ = h.cps.WriteStatus(id, RunStatusRec{
		State: RunStateFailed, Holder: h.replicaID, Error: reason,
		StartedAt: time.Now(), UpdatedAt: time.Now(),
	})
}

// ClaimNexter is the optional fast path a durable store implements for intake:
// atomically move one run from `pending` to `claimed` for holder and return it.
// mhl-store-postgres can back this with `SELECT ... FOR UPDATE SKIP LOCKED`.
// A store that does not implement it uses claimNextCAS — the generic
// compare-and-swap scan over its status records.
//
// No caller wires this yet: the per-replica claim loop that consumes it is
// Etapa 1 (see mvp/docs/design-runtime-intake-duravel.md). The signature is
// fixed now, before the demo binary is frozen, so it is not retrofitted onto
// every store extension later.
type ClaimNexter interface {
	ClaimNext(ctx context.Context, holder string) (runID string, rec RunStatusRec, ok bool, err error)
}

// ErrNoClaimBackend is returned by claimNext when the store neither implements
// ClaimNexter nor offers a CAS-capable KV to run the generic path against.
var ErrNoClaimBackend = errors.New("mcpserver: durable intake needs a ClaimNexter store or a cas-capable KV")

// ErrClaimNextUnsupported is what a ClaimNexter returns when the backing store
// does not actually implement the native claim (it structurally satisfies the
// interface but advertised no such capability). claimNext catches it and falls
// back to the generic CAS scan.
var ErrClaimNextUnsupported = errors.New("mcpserver: store has no native claim_next")

// claimNext moves one pending run to claimed for holder. It uses a native
// ClaimNexter fast path (the CheckpointStore's, or the KV's — e.g. Postgres
// `SELECT … FOR UPDATE SKIP LOCKED`) when one is present and supported, else the
// generic CAS scan against kv. ok == false with a nil error means there is no
// pending run right now.
func claimNext(ctx context.Context, store any, kv LockingKVStore, holder string) (runID string, rec RunStatusRec, ok bool, err error) {
	for _, cand := range []any{store, kv} {
		cn, isCN := cand.(ClaimNexter)
		if !isCN {
			continue
		}
		id, r, done, cErr := cn.ClaimNext(ctx, holder)
		if errors.Is(cErr, ErrClaimNextUnsupported) {
			break // this backend can't; fall through to the CAS scan
		}
		return id, r, done, cErr
	}
	if kv == nil || !kv.CASCapable() {
		return "", RunStatusRec{}, false, ErrNoClaimBackend
	}
	return claimNextCAS(ctx, kv, holder, time.Now)
}

// claimNextCAS lists the run status records under kvRunPrefix, takes the oldest
// one in `pending` (by StartedAt), and flips it to `claimed` with a
// CompareAndSwap against the exact bytes it read. A lost race (another replica
// claimed it first) falls through to the next candidate. now is injectable for
// tests.
func claimNextCAS(ctx context.Context, kv LockingKVStore, holder string, now func() time.Time) (string, RunStatusRec, bool, error) {
	keys, err := kv.List(ctx, kvRunPrefix)
	if err != nil {
		return "", RunStatusRec{}, false, err
	}

	type cand struct {
		key, id string
		raw     []byte
		rec     RunStatusRec
	}
	var pending []cand
	for _, k := range keys {
		rest := strings.TrimPrefix(k, kvRunPrefix)
		id, isStatus := strings.CutSuffix(rest, "/status")
		if !isStatus || id == "" || strings.Contains(id, "/") {
			continue
		}
		raw, found, gErr := kv.Get(ctx, k)
		if gErr != nil || !found {
			continue
		}
		var r RunStatusRec
		if json.Unmarshal(raw, &r) != nil || r.State != RunStatePending {
			continue
		}
		pending = append(pending, cand{key: k, id: id, raw: raw, rec: r})
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].rec.StartedAt.Before(pending[j].rec.StartedAt)
	})

	for _, c := range pending {
		claimed := c.rec
		claimed.State = RunStateClaimed
		claimed.Holder = holder
		claimed.UpdatedAt = now()
		swapped, csErr := kv.CompareAndSwap(ctx, c.key, c.raw, claimed)
		if csErr != nil {
			return "", RunStatusRec{}, false, csErr
		}
		if swapped {
			return c.id, claimed, true, nil
		}
	}
	return "", RunStatusRec{}, false, nil
}
