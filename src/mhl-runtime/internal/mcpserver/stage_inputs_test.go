package mcpserver_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHTTPRunAwaitsStageInputsAndTakesThemOnResume(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{
		"onboard.mh": `import "plan.mh"
partial workflow Onboard {
    input who: string
    entry step Hello { goto Plan }
}
`,
		"plan.mh": `partial workflow Onboard stage Plan {
    input plan: string
    var chosen = ""
    step Choose { self.chosen = "${who}:${plan}" }
}
`,
	})
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{
		"name": "Onboard", "arguments": map[string]any{"who": "ana"},
	}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)

	paused := pollRun(t, ts.URL, sid, runID)
	ai, _ := paused["awaiting_inputs"].(map[string]any)
	if paused["state"] != "paused" || ai["stage"] != "Plan" || ai["step"] != "Choose" {
		t.Fatalf("paused = %v", paused)
	}

	// Without the stage's input: refused, still paused.
	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "run/resume", map[string]any{"runId": runID}), nil)
	e, _ := body["error"].(map[string]any)
	if e == nil || e["code"] != float64(-32602) || !strings.Contains(e["message"].(string), "missing plan") {
		t.Fatalf("resume without the input = %v", body)
	}
	if still := pollRun(t, ts.URL, sid, runID); still["state"] != "paused" {
		t.Fatalf("after a refused resume: %v", still)
	}

	_, body = postMCP(t, ts.URL, sid, rpcMap(4, "run/resume", map[string]any{
		"runId": runID, "arguments": map[string]any{"plan": "pro"},
	}), nil)
	if body["result"] == nil {
		t.Fatalf("run/resume: %v", body)
	}
	final := pollRun(t, ts.URL, sid, runID)
	if final["state"] != "completed" || final["vars"].(map[string]any)["chosen"] != "ana:pro" {
		t.Fatalf("final = %v", final)
	}
	if _, ok := final["awaiting_inputs"]; ok {
		t.Fatalf("a completed run awaits nothing: %v", final)
	}
}

// tools/list tells a client where a workflow may pause: the steps calling
// pause(...) and the stages asking for inputs — none for one that never does.
func TestToolsListSaysWhereAWorkflowPauses(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{
		"onboard.mh": `import "plan.mh"
partial workflow Onboard {
    input who: string
    entry step Hello { goto Plan }
}
`,
		"plan.mh": `partial workflow Onboard stage Plan {
    input plan: string
    step Choose {
        if (plan == "custom") { pause("review the custom plan") }
    }
}
`,
		"plain.mh": `workflow Plain {
    step Only { log("hi") }
}
`,
	})
	sid := initHTTPSession(t, ts.URL)
	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "tools/list", map[string]any{}), nil)
	pauses := map[string]any{}
	for _, raw := range body["result"].(map[string]any)["tools"].([]any) {
		tool := raw.(map[string]any)
		meta, _ := tool["_meta"].(map[string]any)
		run, _ := meta["mhl.run"].(map[string]any)
		if run != nil {
			pauses[tool["name"].(string)] = run["pauses"]
		}
	}
	got, _ := json.Marshal(pauses["Onboard"])
	if string(got) != `{"stages":["Plan"],"steps":["Choose"]}` {
		t.Fatalf("Onboard pauses = %s", got)
	}
	if pauses["Plain"] != nil {
		t.Fatalf("Plain never pauses: %v", pauses["Plain"])
	}
}
