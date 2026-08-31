package mcpserver_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/mcpserver"
)

const httpWF = `
pipeline Greet {
    description: "Greets someone by name."
    input name: string
    var greeting = ""
    step Build { greeting = "hello " + name }
}
`

// httpSlowWF's first step blocks ~1s so a run/cancel lands mid-flight.
const httpSlowWF = `
pipeline Slow {
    var done = ""
    step Wait { var r = cmd.exec(["sleep", "1"]) }
    step Finish { done = "ok" }
}
`

// httpGateWF stops at Gate until it is resumed with approved="yes" — the
// HITL pattern over run/start + run/resume.
const httpGateWF = `
pipeline Approval {
    description: "Two-phase workflow with a human-approval gate."
    input approved: string
    checkpoint: {
        enabled: true
        strategy: "per_step"
        ttl: 7d
    }
    var prepared = ""
    step Prepare { prepared = "ready" }
    step Gate { if (approved != "yes") { fail("awaiting approval") } }
    step Finish { prepared = "done" }
}
`

// initHTTPSession runs the legacy handshake and returns the session id.
func initHTTPSession(t *testing.T, url string) string {
	t.Helper()
	resp, _ := postMCP(t, url, "", rpcMap(1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
	}), nil)
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("initialize returned no Mcp-Session-Id")
	}
	return sid
}

// pollRun polls run/status until state is terminal or the deadline passes,
// returning the last status object.
func pollRun(t *testing.T, url, sid, runID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body := postMCP(t, url, sid, rpcMap(9, "run/status", map[string]any{"runId": runID}), nil)
		res, ok := body["result"].(map[string]any)
		if !ok {
			t.Fatalf("run/status returned no result: %v", body)
		}
		switch res["state"] {
		case "completed", "failed", "canceled":
			return res
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not finish in time: %v", runID, res)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func newHTTPServer(t *testing.T, token string, files map[string]string) *httptest.Server {
	return newHTTPServerCfg(t, mcpserver.HTTPConfig{Token: token}, files)
}

func newHTTPServerCfg(t *testing.T, cfg mcpserver.HTTPConfig, files map[string]string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	if files == nil {
		files = map[string]string{"wf.mh": httpWF}
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Dir = dir
	h, err := mcpserver.Handler(cfg, io.Discard)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

// postMCP sends one JSON-RPC message to <ts>/mcp with optional extra headers
// and returns the raw response plus its decoded body (nil body for 202/204).
func postMCP(t *testing.T, url, sid string, msg map[string]any, hdr map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(msg)
	req, err := http.NewRequest(http.MethodPost, url+"/mcp", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	raw, _ := io.ReadAll(resp.Body)
	if len(strings.TrimSpace(string(raw))) == 0 {
		return resp, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		// Non-JSON body (e.g. a plain-text 401/403) — callers that assert on
		// the body only reach here on a 200.
		return resp, nil
	}
	return resp, out
}

func rpcMap(id int, method string, params any) map[string]any {
	m := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	return m
}

func TestHTTPInitializeSessionListCall(t *testing.T) {
	ts := newHTTPServer(t, "", nil)

	resp, body := postMCP(t, ts.URL, "", rpcMap(1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
	}), nil)
	if resp.StatusCode != 200 {
		t.Fatalf("initialize status = %d", resp.StatusCode)
	}
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("initialize response carried no Mcp-Session-Id header")
	}
	if body["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Errorf("negotiated protocolVersion = %v", body["result"])
	}

	// tools/list on the session, with no params._meta — legacy decoration
	// (resultType) must be absent.
	_, body = postMCP(t, ts.URL, sid, rpcMap(2, "tools/list", nil), nil)
	res := body["result"].(map[string]any)
	if _, ok := res["resultType"]; ok {
		t.Errorf("session-mode tools/list should not carry resultType: %v", res)
	}
	tools := res["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "Greet" {
		t.Fatalf("tools/list = %v", tools)
	}

	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "tools/call", map[string]any{
		"name": "Greet", "arguments": map[string]any{"name": "ana"},
	}), nil)
	call := body["result"].(map[string]any)
	if call["isError"] != false {
		t.Fatalf("tools/call isError = %v (%v)", call["isError"], call)
	}
	text := call["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hello ana") {
		t.Errorf("tools/call text = %q", text)
	}
}

func TestHTTPStatelessNoSession(t *testing.T) {
	ts := newHTTPServer(t, "", nil)

	meta := map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	_, body := postMCP(t, ts.URL, "", rpcMap(1, "tools/call", map[string]any{
		"name": "Greet", "arguments": map[string]any{"name": "ana"}, "_meta": meta,
	}), nil)
	res := body["result"].(map[string]any)
	if res["resultType"] != "complete" {
		t.Errorf("stateless resultType = %v, want \"complete\"", res["resultType"])
	}
	si := res["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"].(map[string]any)
	if si["name"] != "mhl" {
		t.Errorf("_meta serverInfo = %v", si)
	}

	// Same call without params._meta and without a session → -32602.
	_, body = postMCP(t, ts.URL, "", rpcMap(2, "tools/call", map[string]any{
		"name": "Greet", "arguments": map[string]any{"name": "ana"},
	}), nil)
	if code := body["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("missing-_meta code = %v, want -32602", code)
	}
}

func TestHTTPUnknownSession(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	resp, _ := postMCP(t, ts.URL, "no-such-session", rpcMap(1, "tools/list", nil), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown session status = %d, want 404", resp.StatusCode)
	}
}

func TestHTTPDeleteSession(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	resp, _ := postMCP(t, ts.URL, "", rpcMap(1, "initialize", map[string]any{"capabilities": map[string]any{}}), nil)
	sid := resp.Header.Get("Mcp-Session-Id")

	del := func() int {
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/mcp", nil)
		req.Header.Set("Mcp-Session-Id", sid)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code := del(); code != http.StatusNoContent {
		t.Fatalf("first DELETE = %d, want 204", code)
	}
	if code := del(); code != http.StatusNotFound {
		t.Fatalf("second DELETE = %d, want 404", code)
	}
	resp, _ = postMCP(t, ts.URL, sid, rpcMap(2, "tools/list", nil), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST after DELETE = %d, want 404", resp.StatusCode)
	}
}

func TestHTTPAuth(t *testing.T) {
	ts := newHTTPServer(t, "s3cr3t", nil)

	resp, _ := postMCP(t, ts.URL, "", rpcMap(1, "initialize", map[string]any{"capabilities": map[string]any{}}), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", resp.StatusCode)
	}
	resp, body := postMCP(t, ts.URL, "", rpcMap(1, "initialize", map[string]any{"capabilities": map[string]any{}}),
		map[string]string{"Authorization": "Bearer s3cr3t"})
	if resp.StatusCode != 200 || body["result"] == nil {
		t.Fatalf("authenticated initialize failed: %d %v", resp.StatusCode, body)
	}
}

func TestHTTPOrigin(t *testing.T) {
	ts := newHTTPServer(t, "", nil)

	resp, _ := postMCP(t, ts.URL, "", rpcMap(1, "initialize", map[string]any{"capabilities": map[string]any{}}),
		map[string]string{"Origin": "http://evil.example"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", resp.StatusCode)
	}
	resp, _ = postMCP(t, ts.URL, "", rpcMap(1, "initialize", map[string]any{"capabilities": map[string]any{}}),
		map[string]string{"Origin": "http://localhost:5173"})
	if resp.StatusCode != 200 {
		t.Fatalf("loopback-origin status = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPHealthEndpoints(t *testing.T) {
	ts := newHTTPServer(t, "s3cr3t", nil)
	// Probes are unauthenticated (no bearer token) and always GET-able.
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestHTTPGetNotAllowed(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	resp, err := http.Get(ts.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Allow"), "POST") {
		t.Errorf("Allow header = %q, want it to list POST", resp.Header.Get("Allow"))
	}
}

func TestHTTPNotificationAccepted(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	resp, _ := postMCP(t, ts.URL, "", rpcMap(1, "initialize", map[string]any{"capabilities": map[string]any{}}), nil)
	sid := resp.Header.Get("Mcp-Session-Id")

	resp, body := postMCP(t, ts.URL, sid, map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized",
	}, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notification status = %d, want 202", resp.StatusCode)
	}
	if body != nil {
		t.Errorf("notification response had a body: %v", body)
	}
}

func TestHTTPUnsupportedProtocolVersionHeader(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	resp, body := postMCP(t, ts.URL, "", rpcMap(1, "initialize", map[string]any{"capabilities": map[string]any{}}),
		map[string]string{"MCP-Protocol-Version": "1999-01-01"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad protocol-version header status = %d, want 400", resp.StatusCode)
	}
	if code := body["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("code = %v, want -32602", code)
	}
}

func TestHTTPParseError(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("parse-error status = %d, want 400", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code := out["error"].(map[string]any)["code"].(float64); code != -32700 {
		t.Errorf("code = %v, want -32700", code)
	}
}

func TestHTTPDiscoverStateless(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	_, body := postMCP(t, ts.URL, "", rpcMap(1, "server/discover", nil), nil)
	res := body["result"].(map[string]any)
	if res["resultType"] != "complete" {
		t.Errorf("server/discover resultType = %v, want \"complete\"", res["resultType"])
	}
	if res["ttlMs"] == nil || res["cacheScope"] != "public" {
		t.Errorf("server/discover missing cache decoration: %v", res)
	}
}

func TestHTTPRunLifecycle(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{
		"name": "Greet", "arguments": map[string]any{"name": "ana"},
	}), nil)
	res := body["result"].(map[string]any)
	runID, _ := res["runId"].(string)
	if runID == "" {
		t.Fatalf("run/start returned no runId: %v", res)
	}
	if res["state"] != "working" && res["state"] != "completed" {
		t.Errorf("run/start state = %v", res["state"])
	}

	final := pollRun(t, ts.URL, sid, runID)
	if final["state"] != "completed" {
		t.Fatalf("final state = %v (%v)", final["state"], final)
	}
	reached := final["reached"].([]any)
	if len(reached) != 1 || reached[0] != "Build" {
		t.Errorf("reached = %v, want [Build]", reached)
	}
	vars := final["vars"].(map[string]any)
	if vars["greeting"] != "hello ana" {
		t.Errorf("vars.greeting = %v", vars["greeting"])
	}
}

func TestHTTPMaxConcurrentRunsQueues(t *testing.T) {
	ts := newHTTPServerCfg(t, mcpserver.HTTPConfig{MaxConcurrentRuns: 1},
		map[string]string{"slow.mh": httpSlowWF})
	sid := initHTTPSession(t, ts.URL)

	start := func(id int) map[string]any {
		_, body := postMCP(t, ts.URL, sid, rpcMap(id, "run/start", map[string]any{
			"name": "Slow",
		}), nil)
		return body["result"].(map[string]any)
	}

	first := start(2)
	if first["state"] != "working" {
		t.Fatalf("first run state = %v, want working", first["state"])
	}
	second := start(3)
	if second["state"] != "queued" {
		t.Fatalf("second run state = %v, want queued", second["state"])
	}
	if second["queuePosition"].(float64) != 0 {
		t.Errorf("queuePosition = %v, want 0", second["queuePosition"])
	}

	// The queued run gets its slot once the first finishes.
	final := pollRun(t, ts.URL, sid, second["runId"].(string))
	if final["state"] != "completed" {
		t.Fatalf("queued run never ran: final = %v", final)
	}
}

func TestHTTPRunLogs(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{"slow.mh": httpSlowWF})
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{"name": "Slow"}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)

	pollRun(t, ts.URL, sid, runID) // let it finish

	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "run/logs", map[string]any{"runId": runID}), nil)
	res := body["result"].(map[string]any)
	text, _ := res["text"].(string)
	if !strings.Contains(text, "step: Wait") || !strings.Contains(text, "step: Finish") {
		t.Fatalf("run/logs text missing step lines:\n%s", text)
	}
	next := res["nextSince"].(float64)
	if next <= 0 {
		t.Errorf("nextSince = %v, want > 0", next)
	}

	// Polling from the end-cursor yields nothing new.
	_, body = postMCP(t, ts.URL, sid, rpcMap(4, "run/logs", map[string]any{"runId": runID, "since": next}), nil)
	if got := body["result"].(map[string]any)["text"].(string); got != "" {
		t.Errorf("tail read = %q, want empty", got)
	}
}

func TestHTTPRunStatusUnknown(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	sid := initHTTPSession(t, ts.URL)
	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/status", map[string]any{"runId": "nope"}), nil)
	if code := body["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("unknown runId code = %v, want -32602", code)
	}
}

func TestHTTPRunCancel(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{"wf.mh": httpWF, "slow.mh": httpSlowWF})
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{"name": "Slow"}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)

	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "run/cancel", map[string]any{"runId": runID}), nil)
	if st := body["result"].(map[string]any)["state"]; st != "canceled" {
		t.Fatalf("run/cancel state = %v, want canceled", st)
	}

	final := pollRun(t, ts.URL, sid, runID)
	if final["state"] != "canceled" {
		t.Errorf("final state after cancel = %v", final["state"])
	}
	// "Finish" must never have run.
	if reached, ok := final["reached"].([]any); ok {
		for _, s := range reached {
			if s == "Finish" {
				t.Errorf("cancelled run still reached Finish: %v", reached)
			}
		}
	}
}

// With --token + --principal-header, runs are isolated per verified principal:
// run/list shows only the caller's own, and a cross-principal run/status is
// answered exactly like an unknown runId.
func TestHTTPPrincipalIsolation(t *testing.T) {
	ts := newHTTPServerCfg(t, mcpserver.HTTPConfig{Token: "gw", PrincipalHeader: "X-Mhl-Principal"}, nil)

	hdr := func(who string) map[string]string {
		return map[string]string{"Authorization": "Bearer gw", "X-Mhl-Principal": who}
	}
	initAs := func(who string) string {
		resp, _ := postMCP(t, ts.URL, "", rpcMap(1, "initialize", map[string]any{
			"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		}), hdr(who))
		return resp.Header.Get("Mcp-Session-Id")
	}
	startAs := func(who, sid string) string {
		_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{
			"name": "Greet", "arguments": map[string]any{"name": who},
		}), hdr(who))
		return body["result"].(map[string]any)["runId"].(string)
	}

	sidA, sidB := initAs("alice"), initAs("bob")
	runA := startAs("alice", sidA)
	_ = startAs("bob", sidB)

	// bob's run/list must not contain alice's run.
	_, body := postMCP(t, ts.URL, sidB, rpcMap(3, "run/list", nil), hdr("bob"))
	for _, r := range body["result"].(map[string]any)["runs"].([]any) {
		if r.(map[string]any)["runId"] == runA {
			t.Fatalf("bob's run/list leaked alice's run %s", runA)
		}
	}

	// bob probing alice's runId → "unknown runId", indistinguishable from a
	// nonexistent one.
	_, body = postMCP(t, ts.URL, sidB, rpcMap(4, "run/status", map[string]any{"runId": runA}), hdr("bob"))
	if code := body["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Fatalf("cross-principal run/status code = %v, want -32602", code)
	}

	// alice can still see her own.
	_, body = postMCP(t, ts.URL, sidA, rpcMap(5, "run/status", map[string]any{"runId": runA}), hdr("alice"))
	if body["result"] == nil {
		t.Fatalf("alice cannot see her own run: %v", body)
	}
}

// Per-method routes (2b): POST /mcp/<method> runs the same dispatch as
// POST /mcp; the body method must match the path, and GET is 405.
func TestHTTPPerMethodRoutes(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	sid := initHTTPSession(t, ts.URL)

	post := func(path, sid, bodyMethod string, params any) (*http.Response, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(rpcMap(7, bodyMethod, params))
		reqr, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(string(b)))
		reqr.Header.Set("Content-Type", "application/json")
		if sid != "" {
			reqr.Header.Set("Mcp-Session-Id", sid)
		}
		resp, err := http.DefaultClient.Do(reqr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return resp, out
	}

	// Matching path + body → normal result.
	resp, body := post("/mcp/run/list", sid, "run/list", map[string]any{
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion":   "2026-07-28",
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		},
	})
	if resp.StatusCode != 200 || body["result"] == nil {
		t.Fatalf("/mcp/run/list matched call: %d %v", resp.StatusCode, body)
	}

	// Path says run/list, body says run/start → -32600, 400.
	resp, body = post("/mcp/run/list", sid, "run/start", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("mismatch status = %d, want 400", resp.StatusCode)
	}
	if code := body["error"].(map[string]any)["code"].(float64); code != -32600 {
		t.Errorf("mismatch code = %v, want -32600", code)
	}

	// GET on a scoped path → 405.
	g, err := http.Get(ts.URL + "/mcp/run/list")
	if err != nil {
		t.Fatal(err)
	}
	g.Body.Close()
	if g.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp/run/list = %d, want 405", g.StatusCode)
	}
}

func TestHTTPRunRequiresProtocolContext(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	// No session, no params._meta → the same -32602 gate as tools/*.
	_, body := postMCP(t, ts.URL, "", rpcMap(1, "run/start", map[string]any{"name": "Greet"}), nil)
	if code := body["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("ungated run/start code = %v, want -32602", code)
	}
}

func TestHTTPRunResume(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{"gate.mh": httpGateWF})
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{
		"name": "Approval", "arguments": map[string]any{"approved": "no"},
	}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)

	stopped := pollRun(t, ts.URL, sid, runID)
	if stopped["state"] != "failed" {
		t.Fatalf("gated run state = %v, want failed (%v)", stopped["state"], stopped)
	}
	if stopped["resumable"] != true {
		t.Errorf("stopped run should be resumable: %v", stopped)
	}
	if stopped["step"] != "Gate" {
		t.Errorf("stopped at step %v, want Gate", stopped["step"])
	}

	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "run/resume", map[string]any{
		"runId": runID, "arguments": map[string]any{"approved": "yes"},
	}), nil)
	if st := body["result"].(map[string]any)["state"]; st != "working" && st != "completed" {
		t.Fatalf("run/resume state = %v", st)
	}

	final := pollRun(t, ts.URL, sid, runID)
	if final["state"] != "completed" {
		t.Fatalf("resumed run state = %v (%v)", final["state"], final)
	}
	if final["vars"].(map[string]any)["prepared"] != "done" {
		t.Errorf("resumed vars = %v", final["vars"])
	}
}

// TestHTTPRunResumeAcrossProcess simulates a restart: server B, pointed at
// the same --state-dir, resumes a run server A started and never finished.
func TestHTTPRunResumeAcrossProcess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gate.mh"), []byte(httpGateWF), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()

	serverWith := func() *httptest.Server {
		h, err := mcpserver.HandlerWithState(t.Context(), mcpserver.HTTPConfig{Dir: dir, StateDir: stateDir}, io.Discard)
		if err != nil {
			t.Fatalf("HandlerWithState: %v", err)
		}
		s := httptest.NewServer(h)
		t.Cleanup(s.Close)
		return s
	}

	a := serverWith()
	sidA := initHTTPSession(t, a.URL)
	_, body := postMCP(t, a.URL, sidA, rpcMap(2, "run/start", map[string]any{
		"name": "Approval", "arguments": map[string]any{"approved": "no"},
	}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)
	if s := pollRun(t, a.URL, sidA, runID)["state"]; s != "failed" {
		t.Fatalf("run on A ended %v, want failed", s)
	}
	a.Close()

	b := serverWith()
	sidB := initHTTPSession(t, b.URL)

	// B never saw run/start — status must come from disk.
	_, body = postMCP(t, b.URL, sidB, rpcMap(2, "run/status", map[string]any{"runId": runID}), nil)
	st := body["result"].(map[string]any)
	if st["state"] != "failed" || st["resumable"] != true || st["step"] != "Gate" {
		t.Fatalf("reconstructed status = %v", st)
	}

	_, body = postMCP(t, b.URL, sidB, rpcMap(3, "run/resume", map[string]any{
		"runId": runID, "arguments": map[string]any{"approved": "yes"},
	}), nil)
	if body["result"] == nil {
		t.Fatalf("run/resume on B failed: %v", body)
	}
	final := pollRun(t, b.URL, sidB, runID)
	if final["state"] != "completed" {
		t.Fatalf("resumed-on-B state = %v (%v)", final["state"], final)
	}
}

// With a verified principal, the post-restart reclaim is bound: only the
// original principal can pick a run back up from disk.
func TestHTTPReconstructBoundToPrincipal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gate.mh"), []byte(httpGateWF), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	cfg := mcpserver.HTTPConfig{Dir: dir, StateDir: stateDir, Token: "gw", PrincipalHeader: "X-Mhl-Principal"}
	hdr := func(who string) map[string]string {
		return map[string]string{"Authorization": "Bearer gw", "X-Mhl-Principal": who}
	}
	serverWith := func() *httptest.Server {
		h, err := mcpserver.HandlerWithState(t.Context(), cfg, io.Discard)
		if err != nil {
			t.Fatalf("HandlerWithState: %v", err)
		}
		s := httptest.NewServer(h)
		t.Cleanup(s.Close)
		return s
	}
	initAs := func(url, who string) string {
		resp, _ := postMCP(t, url, "", rpcMap(1, "initialize", map[string]any{
			"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		}), hdr(who))
		return resp.Header.Get("Mcp-Session-Id")
	}

	a := serverWith()
	sidA := initAs(a.URL, "alice")
	_, body := postMCP(t, a.URL, sidA, rpcMap(2, "run/start", map[string]any{
		"name": "Approval", "arguments": map[string]any{"approved": "no"},
	}), hdr("alice"))
	runID := body["result"].(map[string]any)["runId"].(string)
	pollRun2(t, a.URL, sidA, runID, hdr("alice"))
	a.Close()

	b := serverWith()
	// bob names alice's runId → unknown (the persisted owner does not match).
	sidBob := initAs(b.URL, "bob")
	_, body = postMCP(t, b.URL, sidBob, rpcMap(3, "run/status", map[string]any{"runId": runID}), hdr("bob"))
	if code := body["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Fatalf("bob reclaimed alice's run: %v", body)
	}
	// alice, on the fresh process, still can.
	sidAlice := initAs(b.URL, "alice")
	_, body = postMCP(t, b.URL, sidAlice, rpcMap(4, "run/status", map[string]any{"runId": runID}), hdr("alice"))
	if body["result"] == nil {
		t.Fatalf("alice cannot reclaim her own run after restart: %v", body)
	}
}

// pollRun2 is pollRun with caller-supplied headers.
func pollRun2(t *testing.T, url, sid, runID string, hdr map[string]string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body := postMCP(t, url, sid, rpcMap(9, "run/status", map[string]any{"runId": runID}), hdr)
		res := body["result"].(map[string]any)
		switch res["state"] {
		case "completed", "failed", "canceled":
			return res
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not finish: %v", runID, res)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestHTTPRunResumeUnknown(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{"gate.mh": httpGateWF})
	sid := initHTTPSession(t, ts.URL)
	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/resume", map[string]any{"runId": "nope"}), nil)
	if code := body["error"].(map[string]any)["code"].(float64); code != -32602 {
		t.Errorf("resume unknown runId code = %v, want -32602", code)
	}
}

// TestHTTPRunOwnership: a run started by session A is invisible and
// untouchable to session B — same server, same (absent) token.
func TestHTTPRunOwnership(t *testing.T) {
	ts := newHTTPServer(t, "", map[string]string{"gate.mh": httpGateWF})
	sidA := initHTTPSession(t, ts.URL)
	sidB := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sidA, rpcMap(2, "run/start", map[string]any{
		"name": "Approval", "arguments": map[string]any{"approved": "no"},
	}), nil)
	runID := body["result"].(map[string]any)["runId"].(string)
	pollRun(t, ts.URL, sidA, runID) // let it stop at the gate

	// B cannot see A's run in run/list.
	_, body = postMCP(t, ts.URL, sidB, rpcMap(3, "run/list", nil), nil)
	if runs := body["result"].(map[string]any)["runs"].([]any); len(runs) != 0 {
		t.Errorf("session B run/list leaked %d run(s): %v", len(runs), runs)
	}
	// A can.
	_, body = postMCP(t, ts.URL, sidA, rpcMap(4, "run/list", nil), nil)
	if runs := body["result"].(map[string]any)["runs"].([]any); len(runs) != 1 {
		t.Fatalf("session A run/list = %d runs, want 1", len(runs))
	}

	// B's status / cancel / resume on A's runId all look like "unknown".
	for i, method := range []string{"run/status", "run/cancel", "run/resume"} {
		_, body = postMCP(t, ts.URL, sidB, rpcMap(10+i, method, map[string]any{"runId": runID}), nil)
		if body["result"] != nil {
			t.Errorf("%s by non-owner returned a result: %v", method, body["result"])
		}
		if code := body["error"].(map[string]any)["code"].(float64); code != -32602 {
			t.Errorf("%s by non-owner code = %v, want -32602", method, code)
		}
	}

	// A still owns it and can resume.
	_, body = postMCP(t, ts.URL, sidA, rpcMap(20, "run/resume", map[string]any{
		"runId": runID, "arguments": map[string]any{"approved": "yes"},
	}), nil)
	if body["result"] == nil {
		t.Fatalf("owner resume failed: %v", body)
	}
	if pollRun(t, ts.URL, sidA, runID)["state"] != "completed" {
		t.Errorf("owner-resumed run did not complete")
	}
}

// tools/call enforces the advertised inputSchema: a missing required argument
// is -32602 naming the field, not a run that executes and fails on a step.
func TestHTTPToolsCallEnforcesInputSchema(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "tools/call", map[string]any{
		"name": "Greet", "arguments": map[string]any{},
	}), nil)
	if body["result"] != nil {
		t.Fatalf("tools/call with no args returned a result: %v", body["result"])
	}
	e := body["error"].(map[string]any)
	if e["code"].(float64) != -32602 {
		t.Errorf("code = %v, want -32602", e["code"])
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, `"name"`) {
		t.Errorf("message %q does not name the missing input", msg)
	}

	// An undeclared argument is rejected too (additionalProperties:false).
	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "tools/call", map[string]any{
		"name": "Greet", "arguments": map[string]any{"name": "ana", "extra": 1},
	}), nil)
	if body["error"].(map[string]any)["code"].(float64) != -32602 {
		t.Errorf("undeclared arg: code = %v, want -32602", body["error"])
	}
}

// run/start rejects a malformed call before any run is registered: -32602, and
// run/list stays empty.
func TestHTTPRunStartEnforcesInputSchema(t *testing.T) {
	ts := newHTTPServer(t, "", nil)
	sid := initHTTPSession(t, ts.URL)

	_, body := postMCP(t, ts.URL, sid, rpcMap(2, "run/start", map[string]any{
		"name": "Greet", "arguments": map[string]any{"wrong": "x"},
	}), nil)
	if body["result"] != nil {
		t.Fatalf("run/start with a bad arg returned a result: %v", body["result"])
	}
	if body["error"].(map[string]any)["code"].(float64) != -32602 {
		t.Errorf("code = %v, want -32602", body["error"])
	}

	_, body = postMCP(t, ts.URL, sid, rpcMap(3, "run/list", nil), nil)
	if runs := body["result"].(map[string]any)["runs"].([]any); len(runs) != 0 {
		t.Errorf("a rejected run/start still registered %d run(s): %v", len(runs), runs)
	}
}
