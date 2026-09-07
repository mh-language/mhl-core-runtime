package mcpserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
)

// intakeRec is the durable record run/start writes for a run before it executes,
// when a cas-capable store is configured (h.claimKV). It carries everything a
// replica needs to run the workflow after claiming it: which workflow, the
// inputs, and the owner/principal for the run/* authorization guards. It is
// written once and read by the claim loop (Etapa 1) or reconstructRun; the
// per-poll status lives separately in the RunStatusRec.
type intakeRec struct {
	Tool      string         `json:"tool"`
	Args      map[string]any `json:"args,omitempty"`
	Owner     Owner          `json:"owner,omitempty"`
	Principal string         `json:"principal,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

func intakeKey(runID string) string { return runKey(runID, "intake") }

// writeIntake persists rn's intake record through the cas store. Args are stored
// through the checkpoint redaction (a credential passed as a literal input is
// kept as a re-resolvable reference, everything else masked — the same contract
// a checkpoint follows, P0-4). A no-op when durable intake is off.
func (h *httpServer) writeIntake(ctx context.Context, rn *asyncRun) error {
	if h.claimKV == nil {
		return nil
	}
	rn.mu.Lock()
	rec := intakeRec{
		Tool:      rn.tool.Name,
		Args:      runtime.RedactVarsForCheckpoint(rn.args),
		Owner:     rn.owner,
		Principal: rn.principal,
		CreatedAt: rn.started,
	}
	rn.mu.Unlock()
	return h.claimKV.Put(ctx, intakeKey(rn.id), rec)
}

// readIntake loads a run's intake record, rehydrating any credential references
// in its args. ok is false when there is no record or it cannot be read.
func (h *httpServer) readIntake(ctx context.Context, runID string) (intakeRec, bool) {
	if h.claimKV == nil {
		return intakeRec{}, false
	}
	raw, found, err := h.claimKV.Get(ctx, intakeKey(runID))
	if err != nil || !found {
		return intakeRec{}, false
	}
	var rec intakeRec
	if json.Unmarshal(raw, &rec) != nil {
		return intakeRec{}, false
	}
	if rec.Args != nil {
		if vars, rErr := runtime.RehydrateVars(rec.Args); rErr == nil {
			rec.Args = vars
		}
	}
	return rec, true
}
