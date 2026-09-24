package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/mcpserver"
)

const typedWorkflowSrc = `
type ReviewInput = { diff: string, base?: string }
type ReviewOutput = { approved: bool, summary: string }

workflow Review(req: ReviewInput): ReviewOutput {
    var summary = ""
    step S {
        if (req.diff == "boom") { fail("bad diff") }
        summary = req.diff + "@" + (req.base ?? "main")
    }
    output: { approved: true, summary: summary }
}

pipeline Plain {
    input name: string
    step S { log(name) }
}
`

// serveTyped runs the stdio server over a directory holding typedWorkflowSrc
// and returns each reply's result keyed by request id.
func serveTyped(t *testing.T, reqs ...string) map[float64]map[string]any {
	t.Helper()
	return serveTypedSrc(t, typedWorkflowSrc, reqs...)
}

// serveTypedSrc is serveTyped over an arbitrary source.
func serveTypedSrc(t *testing.T, src string, reqs ...string) map[float64]map[string]any {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := append([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}, reqs...)
	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	byID := map[float64]map[string]any{}
	for _, m := range decodeLines(t, out.String()) {
		id, _ := m["id"].(float64)
		res, _ := m["result"].(map[string]any)
		byID[id] = res
	}
	return byID
}

func TestTypedWorkflowAdvertisesOutputSchema(t *testing.T) {
	res := serveTyped(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := map[string]map[string]any{}
	for _, raw := range res[2]["tools"].([]any) {
		tool := raw.(map[string]any)
		tools[tool["name"].(string)] = tool
	}

	out, ok := tools["Review"]["outputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("typed workflow has no outputSchema: %v", tools["Review"])
	}
	if out["title"] != "ReviewOutput" || out["type"] != "object" {
		t.Fatalf("outputSchema = %v", out)
	}
	in := tools["Review"]["inputSchema"].(map[string]any)
	if !reflect.DeepEqual(in["required"], []any{"diff"}) || in["additionalProperties"] != false {
		t.Fatalf("inputSchema = %v, want the param type's fields with only diff required", in)
	}
	if _, has := tools["Plain"]["outputSchema"]; has {
		t.Fatalf("untyped pipeline must not advertise an outputSchema: %v", tools["Plain"])
	}
}

func TestTypedWorkflowCallResults(t *testing.T) {
	res := serveTyped(t,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Review","arguments":{"diff":"d1"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Review","arguments":{"diff":"boom"}}}`,
	)

	ok := res[2]
	if ok["isError"] != false {
		t.Fatalf("call failed: %v", ok)
	}
	want := map[string]any{"approved": true, "summary": "d1@main"}
	if !reflect.DeepEqual(ok["structuredContent"], want) {
		t.Fatalf("structuredContent = %v, want exactly the typed projection %v", ok["structuredContent"], want)
	}

	failed := res[3]
	if failed["isError"] != true {
		t.Fatalf("want an error result: %v", failed)
	}
	if _, has := failed["structuredContent"]; has {
		t.Fatalf("an error from a tool with an outputSchema must carry no structuredContent (it would not conform): %v", failed)
	}
	text, _ := json.Marshal(failed["content"])
	if !strings.Contains(string(text), "bad diff") {
		t.Fatalf("error detail must stay in the text content: %s", text)
	}
}

func TestTypedWorkflowManifest(t *testing.T) {
	res := serveTyped(t, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"mhl://workflow/Review"}}`)
	contents := res[2]["contents"].([]any)
	var man map[string]any
	if err := json.Unmarshal([]byte(contents[0].(map[string]any)["text"].(string)), &man); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(man["input"], map[string]any{"param": "req", "type": "ReviewInput"}) {
		t.Errorf("manifest input = %v", man["input"])
	}
	if !reflect.DeepEqual(man["output"], map[string]any{"type": "ReviewOutput"}) {
		t.Errorf("manifest output = %v", man["output"])
	}
	if _, ok := man["outputSchema"].(map[string]any); !ok {
		t.Errorf("manifest has no outputSchema: %v", man)
	}
	for _, raw := range man["inputs"].([]any) {
		in := raw.(map[string]any)
		if want := in["name"] == "diff"; in["required"] != want {
			t.Errorf("manifest input %v: required = %v, want %v", in["name"], in["required"], want)
		}
	}
}
