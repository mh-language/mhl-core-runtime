package mcpserver

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

func TestCheckRunInputs(t *testing.T) {
	if err := checkRunInputs(nil); err != nil {
		t.Errorf("nil args: %v", err)
	}
	if err := checkRunInputs(map[string]any{"claim": "c-1", "n": 3}); err != nil {
		t.Errorf("plain JSON args: %v", err)
	}

	// Over the size cap → rejected.
	big := map[string]any{"blob": strings.Repeat("x", maxRunInputBytes)}
	if err := checkRunInputs(big); err == nil {
		t.Error("oversize inputs accepted, want rejection")
	}

	// Not JSON-serialisable → rejected as ErrNonSerializableVar (defence for the
	// in-process caller; the MCP transport can't produce this).
	err := checkRunInputs(map[string]any{"fn": func() {}})
	if !errors.Is(err, runtime.ErrNonSerializableVar) {
		t.Errorf("closure input: err = %v, want ErrNonSerializableVar", err)
	}
}

func TestRunStateHelpers(t *testing.T) {
	cases := []struct {
		s            RunState
		terminal     bool
		preExecution bool
		valid        bool
	}{
		{RunStatePending, false, true, true},
		{RunStateClaimed, false, true, true},
		{RunStateQueued, false, true, true},
		{RunStateWorking, false, false, true},
		{RunStatePaused, false, false, true},
		{RunStateCompleted, true, false, true},
		{RunStateFailed, true, false, true},
		{RunStateCanceled, true, false, true},
		{RunState("bogus"), false, false, false},
		{RunState(""), false, false, false},
	}
	for _, c := range cases {
		if got := c.s.IsTerminal(); got != c.terminal {
			t.Errorf("%q.IsTerminal() = %v, want %v", c.s, got, c.terminal)
		}
		if got := c.s.IsPreExecution(); got != c.preExecution {
			t.Errorf("%q.IsPreExecution() = %v, want %v", c.s, got, c.preExecution)
		}
		if got := c.s.Valid(); got != c.valid {
			t.Errorf("%q.Valid() = %v, want %v", c.s, got, c.valid)
		}
	}
}

// mkMsg / decodeResult are white-box helpers to drive a run/* handler directly.
func mkMsg(method string, params any) rpcMsg {
	raw, _ := json.Marshal(params)
	return rpcMsg{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method, Params: raw}
}

func decodeResult(t *testing.T, m *rpcMsg) map[string]any {
	t.Helper()
	if m.Error != nil {
		t.Fatalf("rpc error: code=%d msg=%q", m.Error.Code, m.Error.Message)
	}
	var out map[string]any
	if err := json.Unmarshal(m.Result, &out); err != nil {
		t.Fatalf("decode result: %v (%s)", err, m.Result)
	}
	return out
}

// A run in a pre-execution state (pending/claimed) exists but has not started:
// run/status reports it plainly, run/logs is empty, run/cancel stops it without
// a running goroutine and without it ever executing. Nothing produces these
// states yet (durable intake is Etapa 1) — this fixes the contract before the
// demo binary is frozen.
func TestPreExecutionRunStatusLogsCancelCoherent(t *testing.T) {
	for _, state := range []RunState{RunStatePending, RunStateClaimed} {
		t.Run(string(state), func(t *testing.T) {
			h := buildTestHTTP(t, HTTPConfig{})
			sess := &session{id: "s1"}
			var w execsvc.Workflow
			for _, x := range h.srv.tools {
				w = x
				break
			}
			if w.Name == "" {
				t.Fatal("no tool registered")
			}

			rn := &asyncRun{
				id:      "rp",
				owner:   h.ownerOf(sess),
				tool:    w,
				state:   state,
				started: time.Now(),
				updated: time.Now(),
				cancel:  func() {},
				done:    make(chan struct{}),
				logs:    newRingLog(),
			}
			h.runs.Put(rn)

			// run/status: plain, no error/reason/resumable.
			st := decodeResult(t, h.runStatus(sess, mkMsg("run/status", map[string]any{"runId": "rp"})))
			if st["state"] != string(state) {
				t.Fatalf("status state = %v, want %q", st["state"], state)
			}
			for _, k := range []string{"error", "reason", "resumable", "step"} {
				if _, ok := st[k]; ok {
					t.Errorf("status unexpectedly carries %q: %v", k, st[k])
				}
			}

			// run/logs: empty, cursor at 0.
			lg := decodeResult(t, h.runLogs(sess, mkMsg("run/logs", map[string]any{"runId": "rp"})))
			if lg["text"] != "" {
				t.Errorf("logs text = %q, want empty", lg["text"])
			}
			if n, _ := lg["nextSince"].(float64); n != 0 {
				t.Errorf("logs nextSince = %v, want 0", lg["nextSince"])
			}

			// run/cancel: → canceled, and the run never ran.
			cn := decodeResult(t, h.runCancel(sess, mkMsg("run/cancel", map[string]any{"runId": "rp"})))
			if cn["state"] != string(RunStateCanceled) {
				t.Fatalf("cancel state = %v, want %q", cn["state"], RunStateCanceled)
			}
			rn.mu.Lock()
			got, reached := rn.state, len(rn.reached)
			rn.mu.Unlock()
			if got != RunStateCanceled {
				t.Errorf("run state after cancel = %q, want canceled", got)
			}
			if reached != 0 {
				t.Errorf("run executed %d steps; a pre-execution cancel must not run anything", reached)
			}
		})
	}
}
