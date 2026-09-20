package mcpserver_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/mcpserver"
)

// TestServePartialWorkflowDirectoryListsAndRunsOnlyTheAssembledTool is the
// end-to-end confidence check for `partial` under `mhl serve mcp <dir>`:
// a directory holding both the primary file (with `entry step`) and its
// fragment (pulled in via a whole-file `import`) must expose exactly one
// "Discovery" tool — not two, not a dangling incomplete one from the
// fragment file execsvc.Load also scans independently — and calling it
// must actually run the merged step set, entry step first. An unrelated
// ordinary pipeline in the same directory proves a partial fragment never
// takes the rest of the directory down with it (the regression Load()
// itself is separately tested for in internal/execsvc).
func TestServePartialWorkflowDirectoryListsAndRunsOnlyTheAssembledTool(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"fragment.mh": `
partial workflow Discovery {
    step Gate {
        gate_reached = true
    }
}
`,
		"main.mh": `
import "fragment.mh"

partial workflow Discovery {
    description: "Splits a workflow across files."
    var dispatch_reached = false
    var gate_reached = false

    entry step Dispatch {
        dispatch_reached = true
        goto Gate
    }
}
`,
		"other.mh": `
pipeline Other {
    step S {
        log("other ran")
    }
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Discovery","arguments":{}}}`,
	}, "\n") + "\n")

	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.String())
	if len(msgs) != 3 {
		t.Fatalf("got %d responses, want 3:\n%s", len(msgs), out.String())
	}

	tools := msgs[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools/list returned %d tools, want 2 (Discovery, Other) — got %+v", len(tools), tools)
	}
	names := map[string]map[string]any{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		names[tool["name"].(string)] = tool
	}
	discovery, ok := names["Discovery"]
	if !ok {
		t.Fatalf("expected a \"Discovery\" tool, got %+v", names)
	}
	if discovery["description"] != "Splits a workflow across files." {
		t.Errorf("Discovery description = %v", discovery["description"])
	}
	if _, ok := names["Other"]; !ok {
		t.Fatalf("expected the unrelated \"Other\" pipeline to still be registered, got %+v", names)
	}

	callRes := msgs[2]["result"].(map[string]any)
	if callRes["isError"] != false {
		t.Fatalf("isError = %v, want false; result: %v", callRes["isError"], callRes)
	}
	// structuredContent carries the final vars: both dispatch_reached (set
	// by the entry step, declared in main.mh) and gate_reached (set by
	// Gate, declared in fragment.mh, reached only via the entry step's
	// `goto`) must be true — proof the merged, cross-file step graph
	// actually ran end to end through the real MCP `tools/call` path, not
	// just via the lower-level runtime/execsvc APIs the other partial
	// tests already exercise.
	structured := callRes["structuredContent"].(map[string]any)
	if structured["dispatch_reached"] != true {
		t.Errorf("dispatch_reached = %v, want true", structured["dispatch_reached"])
	}
	if structured["gate_reached"] != true {
		t.Errorf("gate_reached = %v, want true", structured["gate_reached"])
	}
}

const httpPartialApprovalMain = `
import "httpPartialApprovalFragment.mh"

partial workflow Approval {
    input approved: string
    var prepared = ""
    entry step Prepare { prepared = "ready" }
    step Gate { if (approved != "yes") { pause("awaiting human approval") } }
}
`

const httpPartialApprovalFragment = `
partial workflow Approval {
    step Finish { prepared = "done" }
}
`

// TestHTTPRunPauseThenResumeAcrossPartialFragments is
// TestHTTPRunPauseThenResume's `partial` counterpart, and the sharpest
// available proof that a `partial` workflow's checkpoint digest
// (runtime.DefinitionDigest, computed over the already-merged Pipeline —
// see execsvc.Run) is stable across a real pause/resume round trip: if
// resolvePartials's merge were non-deterministic, or FindPipeline's
// entry-step reordering weren't applied identically on every resolve, the
// digest computed at `run/start` and the one recomputed at `run/resume`
// would disagree and resume would be refused with a definition-mismatch
// error instead of completing. It also proves resume genuinely continues
// into a step declared in a *different* file than the one it paused in —
// `Gate` (main file) pauses, `Finish` (fragment file) is what the resumed
// run reaches.
func TestHTTPRunPauseThenResumeAcrossPartialFragments(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{
		"main.mh":                        httpPartialApprovalMain,
		"httpPartialApprovalFragment.mh": httpPartialApprovalFragment,
	})
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{
		"name": "Approval", "arguments": map[string]any{"approved": "no"},
	}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)

	paused := pollRun(t, ts.URL, sid, runID)
	if paused["state"] != "paused" {
		t.Fatalf("state = %v, want paused (%v)", paused["state"], paused)
	}
	if paused["step"] != "Gate" {
		t.Errorf("paused at step %v, want Gate", paused["step"])
	}

	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "run/resume", map[string]any{
		"runId": runID, "arguments": map[string]any{"approved": "yes"},
	}), nil)
	if body["result"] == nil {
		t.Fatalf("run/resume failed (a definition-digest mismatch would land here): %v", body)
	}

	final := pollRun(t, ts.URL, sid, runID)
	if final["state"] != "completed" {
		t.Fatalf("resumed run state = %v (%v)", final["state"], final)
	}
	if final["vars"].(map[string]any)["prepared"] != "done" {
		t.Errorf("resumed vars = %v, want prepared=done (set by Finish, in the fragment file)", final["vars"])
	}
}
