package mcpserver_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// typedGateWF is a typed workflow with a HITL gate: the approval arrives as
// an optional field of the param, so run/resume supplies it as a partial
// argument set that must be merged onto the checkpointed `req`.
const typedGateWF = `
type ReviewInput = { diff: string, base?: string, approved?: string }
type ReviewOutput = { approved: bool, summary: string }

workflow Review(req: ReviewInput): ReviewOutput {
    var summary = ""
    step Prepare { summary = req.diff + "@" + (req.base ?? "main") }
    step Gate { if (req.approved != "yes") { pause("awaiting approval") } }
    step Finish { summary = summary + " ok" }
    output: {
        approved: req.approved == "yes"
        summary: summary + " base=" + (req.base ?? "main")
    }
}

type Status = { ok: bool }

workflow Broken: Status {
    var result = { ok: "yes" }
    step S { log("ran") }
    output: result
}
`

func TestHTTPTypedRunPauseResumeMergesParam(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{"review.mh": typedGateWF})
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{
		"name": "Review", "arguments": map[string]any{"diff": "d1"},
	}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)

	paused := pollRun(t, ts.URL, sid, runID)
	if paused["state"] != "paused" || paused["step"] != "Gate" {
		t.Fatalf("want paused at Gate, got %v", paused)
	}

	// Only the new fields: diff must survive from the checkpointed req.
	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "run/resume", map[string]any{
		"runId": runID, "arguments": map[string]any{"approved": "yes", "base": "dev"},
	}), nil)
	if body["result"] == nil {
		t.Fatalf("run/resume failed: %v", body)
	}

	final := pollRun(t, ts.URL, sid, runID)
	if final["state"] != "completed" {
		t.Fatalf("resumed run state = %v (%v)", final["state"], final)
	}
	want := map[string]any{"approved": true, "summary": "d1@main ok base=dev"}
	if !reflect.DeepEqual(final["vars"], want) {
		t.Fatalf("final vars = %v, want the checked projection %v", final["vars"], want)
	}

	_, body = postMCP(t, ts.URL, sid, rpcMap(4, "resources/read", map[string]any{
		"uri": "mhl://run/" + runID + "/result",
	}), nil)
	contents, _ := body["result"].(map[string]any)["contents"].([]any)
	if len(contents) == 0 {
		t.Fatalf("run result resource empty: %v", body)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(contents[0].(map[string]any)["text"].(string)), &result); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result["vars"], want) {
		t.Fatalf("mhl://run/%s/result vars = %v, want %v", runID, result["vars"], want)
	}
}

func TestHTTPTypedRunOutputContractFails(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{"review.mh": typedGateWF})
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{"name": "Broken"}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)

	final := pollRun(t, ts.URL, sid, runID)
	if final["state"] != "failed" {
		t.Fatalf("state = %v, want failed (%v)", final["state"], final)
	}
	if msg, _ := final["error"].(string); !strings.Contains(msg, "does not satisfy Status") {
		t.Fatalf("error = %q, want the output-contract message", msg)
	}
	if v, ok := final["vars"]; ok && v != nil && len(v.(map[string]any)) > 0 {
		t.Fatalf("a contract failure must not report a result: vars = %v", v)
	}
}

func TestTypedToolCallPausedHasNoStructuredContent(t *testing.T) {
	res := serveTypedSrc(t, typedGateWF,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Review","arguments":{"diff":"d1"}}}`,
	)
	r := res[2]
	if r["isError"] != false {
		t.Fatalf("a pause is not an error: %v", r)
	}
	if _, has := r["structuredContent"]; has {
		t.Fatalf("a paused typed tool must carry no structuredContent (partial state can't satisfy outputSchema): %v", r)
	}
}
