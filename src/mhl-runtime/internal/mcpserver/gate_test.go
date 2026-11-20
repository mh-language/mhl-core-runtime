package mcpserver_test

import (
	"strings"
	"testing"
)

const httpTypedGateWF = `
enum Action { Approve, Reject }
type Review = { action: Action }

partial workflow Reviewed stage Approval {
    var outcome = ""
    entry step Gate {
        var d = pause("review it", decision: Review)
        self.outcome = if (d.action == Action.Approve) "approved" else "rejected"
    }
}
`

func TestHTTPRunGateAwaitsAndTakesADecision(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{"gate.mh": httpTypedGateWF})
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{"name": "Reviewed"}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)

	paused := pollRun(t, ts.URL, sid, runID)
	awaiting, _ := paused["awaiting"].(map[string]any)
	if paused["state"] != "paused" || awaiting["step"] != "Gate" || awaiting["type"] != "Review" {
		t.Fatalf("paused = %v", paused)
	}
	if paused["stage"] != "Approval" {
		t.Fatalf("run/status names the stage it is in: %v", paused)
	}
	if _, ok := awaiting["schema"].(map[string]any); !ok {
		t.Fatalf("awaiting carries no schema: %v", awaiting)
	}

	// A decision of the wrong type is refused; the run stays paused.
	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "run/resume", map[string]any{
		"runId": runID, "decision": map[string]any{"action": "Maybe"},
	}), nil)
	e, _ := body["error"].(map[string]any)
	if e == nil || e["code"] != float64(-32602) || !strings.Contains(e["message"].(string), "not a variant") {
		t.Fatalf("bad decision = %v", body)
	}
	if still := pollRun(t, ts.URL, sid, runID); still["state"] != "paused" {
		t.Fatalf("after a refused decision: %v", still)
	}

	_, body = postMCP(t, ts.URL, sid, rpcMap(4, "run/resume", map[string]any{
		"runId": runID, "decision": map[string]any{"action": "Approve"},
	}), nil)
	if body["result"] == nil {
		t.Fatalf("run/resume: %v", body)
	}
	final := pollRun(t, ts.URL, sid, runID)
	if final["state"] != "completed" || final["vars"].(map[string]any)["outcome"] != "approved" {
		t.Fatalf("final = %v", final)
	}
	if _, ok := final["awaiting"]; ok {
		t.Fatalf("a completed run awaits nothing: %v", final)
	}

	// A completed run takes no decision.
	_, body = postMCP(t, ts.URL, sid, rpcMap(5, "run/resume", map[string]any{
		"runId": runID, "decision": map[string]any{"action": "Approve"},
	}), nil)
	if body["error"] == nil {
		t.Fatalf("decision for a completed run = %v", body)
	}
}
