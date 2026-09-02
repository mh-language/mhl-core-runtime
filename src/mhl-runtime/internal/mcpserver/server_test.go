package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/mcpserver"
)

func decodeLines(t *testing.T, s string) []map[string]any {
	t.Helper()
	var msgs []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(s), "\n") {
		if ln == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("bad JSON line %q: %v", ln, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func TestServeInitializeListCall(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(`
pipeline Greet {
    description: "Greets someone by name."
    input name: string
    var greeting = ""
    step Build { greeting = "hello " + name }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Greet","arguments":{"name":"ana"}}}`,
	}, "\n") + "\n")

	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	msgs := decodeLines(t, out.String())
	if len(msgs) != 3 {
		t.Fatalf("got %d responses, want 3 (the notification is not answered):\n%s", len(msgs), out.String())
	}

	// initialize
	initRes := msgs[0]["result"].(map[string]any)
	if initRes["protocolVersion"] != "2025-06-18" {
		t.Errorf("initialize protocolVersion = %v", initRes["protocolVersion"])
	}
	if si := initRes["serverInfo"].(map[string]any); si["name"] != "mhl" {
		t.Errorf("serverInfo.name = %v", si["name"])
	}
	// stdio does not route run/*, so it must not advertise the mhl.run
	// capability (that is HTTP-only — see TestHTTPAdvertisesAsyncRunCapability).
	if caps, _ := initRes["capabilities"].(map[string]any); caps["experimental"] != nil {
		t.Errorf("stdio initialize advertised experimental capabilities: %v", caps["experimental"])
	}

	// tools/list
	tools := msgs[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools/list returned %d tools, want 1", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "Greet" {
		t.Errorf("tool name = %v", tool["name"])
	}
	if tool["description"] != "Greets someone by name." {
		t.Errorf("tool description = %q, want the workflow's `description:` value", tool["description"])
	}
	req := tool["inputSchema"].(map[string]any)["required"].([]any)
	if len(req) != 1 || req[0] != "name" {
		t.Errorf("inputSchema.required = %v, want [name]", req)
	}

	// tools/call
	callRes := msgs[2]["result"].(map[string]any)
	if callRes["isError"] != false {
		t.Errorf("isError = %v, want false; result: %v", callRes["isError"], callRes)
	}
	text := callRes["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hello ana") {
		t.Errorf("tool result text = %q, want it to contain %q", text, "hello ana")
	}
}

func TestServeUnknownMethodAndTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(
		"pipeline P {\n    step S { var x = 1 }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"does/not/exist"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Nope"}}`,
	}, "\n") + "\n")

	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.String())
	if len(msgs) != 3 {
		t.Fatalf("want 3 responses, got %d", len(msgs))
	}
	if code := msgs[1]["error"].(map[string]any)["code"].(float64); code != -32601 {
		t.Errorf("unknown method code = %v, want -32601", code)
	}
	if code := msgs[2]["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("unknown tool code = %v, want -32602", code)
	}
	if msg := msgs[2]["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "unknown tool") {
		t.Errorf("post-handshake unknown-tool message = %q, want it to mention the tool", msg)
	}
}

// Stateless mode (2026-07-28): a tools/call with the required params._meta
// works with no `initialize` handshake.
func TestServeStatelessWithMeta(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(`
pipeline Greet {
    input name: string
    var greeting = ""
    step Build { greeting = "hi " + name }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{` +
			`"name":"Greet","arguments":{"name":"ana"},` +
			`"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
			`"io.modelcontextprotocol/clientCapabilities":{}}}}` + "\n")

	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.String())
	res, ok := msgs[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("stateless tools/call did not return a result: %v", msgs[0])
	}
	// Modern-mode decorations.
	if res["resultType"] != "complete" {
		t.Errorf("resultType = %v, want \"complete\"", res["resultType"])
	}
	si := res["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"].(map[string]any)
	if si["name"] != "mhl" {
		t.Errorf("_meta serverInfo = %v", si)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hi ana") {
		t.Errorf("result text = %q", text)
	}
	if sc := res["structuredContent"].(map[string]any); sc["greeting"] != "hi ana" {
		t.Errorf("structuredContent = %v, want greeting=\"hi ana\"", sc)
	}
}

// A modern request whose _meta names a protocolVersion this server does not
// implement gets -32022 with the supported list.
func TestServeStatelessUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(
		"pipeline P {\n    step S { var x = 1 }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{` +
			`"io.modelcontextprotocol/protocolVersion":"1999-01-01",` +
			`"io.modelcontextprotocol/clientCapabilities":{}}}}` + "\n")

	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	e := decodeLines(t, out.String())[0]["error"].(map[string]any)
	if e["code"].(float64) != -32022 {
		t.Fatalf("code = %v, want -32022", e["code"])
	}
	data := e["data"].(map[string]any)
	if data["requested"] != "1999-01-01" {
		t.Errorf("data.requested = %v", data["requested"])
	}
	if sup := data["supported"].([]any); len(sup) != 1 || sup[0] != "2026-07-28" {
		t.Errorf("data.supported = %v, want [2026-07-28]", sup)
	}
}

// A stateless request missing params._meta is rejected with -32602 (which is
// also what triggers a client's handshake fallback).
func TestServeStatelessMissingMetaRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(
		"pipeline P {\n    step S { var x = 1 }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")

	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	e := decodeLines(t, out.String())[0]["error"].(map[string]any)
	if e["code"].(float64) != -32602 {
		t.Fatalf("code = %v, want -32602; msg %v", e["code"], e["message"])
	}
}

func TestServeDiscover(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(
		"pipeline P {\n    step S { var x = 1 }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"server/discover"}` + "\n")

	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	res := decodeLines(t, out.String())[0]["result"].(map[string]any)
	if res["resultType"] != "complete" {
		t.Errorf("resultType = %v, want \"complete\"", res["resultType"])
	}
	sv := res["supportedVersions"].([]any)
	if len(sv) != 1 || sv[0] != "2026-07-28" {
		t.Errorf("supportedVersions = %v, want [2026-07-28]", sv)
	}
	si := res["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"].(map[string]any)
	if si["name"] != "mhl" {
		t.Errorf("_meta serverInfo.name = %v", si)
	}
	if _, hasTopLevel := res["serverInfo"]; hasTopLevel {
		t.Errorf("serverInfo must be in _meta, not at the result top level")
	}
	// CacheableResult: ttlMs (>= 0) and cacheScope are required.
	if ttl, ok := res["ttlMs"].(float64); !ok || ttl < 0 {
		t.Errorf("ttlMs = %v, want a non-negative number", res["ttlMs"])
	}
	if res["cacheScope"] != "public" {
		t.Errorf("cacheScope = %v, want \"public\"", res["cacheScope"])
	}
}

// tools/list is a cacheable result in modern mode — ttlMs + cacheScope
// required; a stray cursor is -32602; ping is no longer a method.
func TestServeToolsListCacheAndCursor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(
		"pipeline P {\n    step S { var x = 1 }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`
	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + meta + `}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"cursor":"x",` + meta + `}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping","params":{` + meta + `}}`,
	}, "\n") + "\n")

	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	m := decodeLines(t, out.String())

	list := m[0]["result"].(map[string]any)
	if _, ok := list["ttlMs"].(float64); !ok {
		t.Errorf("tools/list missing ttlMs: %v", list)
	}
	if list["cacheScope"] != "public" {
		t.Errorf("tools/list cacheScope = %v", list["cacheScope"])
	}
	if code := m[1]["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("cursor error code = %v, want -32602", code)
	}
	if code := m[2]["error"].(map[string]any)["code"].(float64); code != -32601 {
		t.Errorf("modern ping code = %v, want -32601 (ping removed in 2026-07-28)", code)
	}
}

func TestServeNoWorkflows(t *testing.T) {
	dir := t.TempDir()
	err := mcpserver.Serve(context.Background(), dir, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected an error when the directory declares no pipeline/workflow")
	}
}

// Workflow resources are transport-shared: over stdio, resources/list enumerates
// a manifest + source per workflow and resources/read returns each.
func TestServeResources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(`
agent Writer { engine: "cli/echo" }
pipeline Greet {
    description: "Greets someone by name."
    input name: string
    checkpoint: { enabled: true, strategy: "per_step", ttl: 7d }
    var greeting = ""
    step Build { greeting = "hello " + name }
    step Emit { log(greeting) }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"mhl://workflow/Greet"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"mhl://workflow/Greet/source"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"mhl://workflow/Nope"}}`,
	}, "\n") + "\n")

	var out bytes.Buffer
	if err := mcpserver.Serve(context.Background(), dir, in, &out, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	msgs := decodeLines(t, out.String())

	// initialize advertises the resources capability.
	caps := msgs[0]["result"].(map[string]any)["capabilities"].(map[string]any)
	if _, ok := caps["resources"].(map[string]any); !ok {
		t.Errorf("initialize did not advertise resources capability: %v", caps)
	}

	// resources/list: manifest + source for Greet.
	res := msgs[1]["result"].(map[string]any)["resources"].([]any)
	uris := map[string]string{}
	for _, r := range res {
		m := r.(map[string]any)
		uris[m["uri"].(string)] = m["mimeType"].(string)
	}
	if uris["mhl://workflow/Greet"] != "application/json" || uris["mhl://workflow/Greet/source"] != "text/x-mhl" {
		t.Errorf("resources/list = %v", res)
	}

	// resources/read manifest — derived detail a tools/list entry omits.
	man := msgs[2]["result"].(map[string]any)["contents"].([]any)[0].(map[string]any)
	var manifest map[string]any
	if err := json.Unmarshal([]byte(man["text"].(string)), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	steps := manifest["steps"].([]any)
	if len(steps) != 2 || steps[0] != "Build" || steps[1] != "Emit" {
		t.Errorf("manifest steps = %v", steps)
	}
	if manifest["resumable"] != true {
		t.Errorf("manifest omitted resumable despite a per_step checkpoint: %v", manifest)
	}
	if cp, _ := manifest["checkpoint"].(map[string]any); cp["strategy"] != "per_step" {
		t.Errorf("manifest checkpoint = %v", manifest["checkpoint"])
	}
	if decl, _ := manifest["declared"].(map[string]any); decl == nil || len(decl["agents"].([]any)) != 1 {
		t.Errorf("manifest declared block = %v", manifest["declared"])
	}

	// resources/read source — the .mh text.
	src := msgs[3]["result"].(map[string]any)["contents"].([]any)[0].(map[string]any)
	if !strings.Contains(src["text"].(string), "pipeline Greet") {
		t.Errorf("source resource text = %q", src["text"])
	}

	// Unknown workflow → -32602.
	if code := msgs[4]["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("unknown resource code = %v, want -32602", code)
	}
}
