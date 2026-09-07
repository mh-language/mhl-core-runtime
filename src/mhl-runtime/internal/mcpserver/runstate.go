package mcpserver

// RunState is the lifecycle state of an async run (run/start … run/status).
// Its underlying type is string, so it marshals to the same JSON the wire
// protocol already used and an untyped string literal still assigns/compares
// to it — existing callers and tests are unaffected by the change of type.
//
// The full state machine (see mvp/docs/design-runtime-intake-duravel.md):
//
//	run/start ─▶ pending ─claim─▶ claimed ─lease─▶ working ─▶ completed
//	              ▲                   │              │ │
//	              │  reconcile: lease │ lost         │ └─▶ failed (resumable)
//	              └───────────────────┴──────────────┘
//	              │                            pause() │ ▲ run/resume
//	              │ run/cancel                         ▼ │
//	              ▼                                  paused
//	          cancelled
//
// pending and claimed have no producer yet: durable intake (Etapa 1) is what
// writes a run as pending and a claim loop is what moves it to claimed. They
// are defined now so the contract — run/status, run/logs and run/cancel
// answering coherently for a run that exists but has not started — is fixed
// before the demo binary is frozen. queued is the pre-existing in-memory
// "waiting for a local concurrency slot" state; it stays distinct from the
// durable pending.
type RunState string

const (
	// RunStatePending: accepted by run/start and recorded durably, not yet
	// claimed by any replica. (Producer: Etapa 1.)
	RunStatePending RunState = "pending"
	// RunStateClaimed: a replica won the atomic claim and is about to acquire
	// the execution lease. (Producer: Etapa 1.)
	RunStateClaimed RunState = "claimed"
	// RunStateQueued: in this replica's memory, waiting for a concurrency slot.
	RunStateQueued RunState = "queued"
	// RunStateWorking: executing steps on this (or, for a remote run, another)
	// replica, holding the execution lease.
	RunStateWorking RunState = "working"
	// RunStatePaused: suspended by pause(...) for a human-in-the-loop hand-off;
	// resumable via run/resume.
	RunStatePaused RunState = "paused"
	// RunStateCompleted: finished cleanly.
	RunStateCompleted RunState = "completed"
	// RunStateFailed: a step failed (resumable when a checkpoint exists).
	RunStateFailed RunState = "failed"
	// RunStateCanceled: stopped by run/cancel, drain deadline, or shutdown.
	RunStateCanceled RunState = "canceled"
)

// IsTerminal reports whether s is an end state — no further transition happens
// without an explicit run/resume.
func (s RunState) IsTerminal() bool {
	switch s {
	case RunStateCompleted, RunStateFailed, RunStateCanceled:
		return true
	default:
		return false
	}
}

// IsPreExecution reports whether s is a state a run can be in before any step
// has run: it is safe to cancel such a run without a running goroutine to stop
// and without any external effect having happened.
func (s RunState) IsPreExecution() bool {
	switch s {
	case RunStatePending, RunStateClaimed, RunStateQueued:
		return true
	default:
		return false
	}
}

// Valid reports whether s is one of the known states.
func (s RunState) Valid() bool {
	switch s {
	case RunStatePending, RunStateClaimed, RunStateQueued, RunStateWorking,
		RunStatePaused, RunStateCompleted, RunStateFailed, RunStateCanceled:
		return true
	default:
		return false
	}
}
