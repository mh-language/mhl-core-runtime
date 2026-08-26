package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMCPServerCallDispatchesToolsCallOverHTTP proves the end-to-end wiring
// added for `<mcp_server>.call(...)`: a declared mcp_server's `http`
// transport, a real JSON-RPC "tools/call" request/response round trip
// through features/mcp.Client, and the decoded result landing back as an
// ordinary MHL value (here, read with plain field access). The fake server
// stands in for a real one (e.g. GitHub's remote MCP server at
// api.githubcopilot.com/mcp/, which speaks this same JSON-RPC-over-HTTP
// shape) — swapping `url` for the real endpoint and `headers` for a live
// token is the only difference from calling it for real.
func TestMCPServerCallDispatchesToolsCallOverHTTP(t *testing.T) {
	var gotAuth, gotMethod, gotToolName string
	var gotArgs map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("server: decoding request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "tools/list" {
			// evalMCPServerToolCall probes this first for x-mcp-header
			// support; an empty catalog is a perfectly valid answer and
			// keeps that lookup a no-op for this test's own assertions.
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
			return
		}
		gotMethod = req.Method
		gotToolName = req.Params.Name
		gotArgs = req.Params.Arguments
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"total_count":2,"query":"` + req.Params.Arguments["query"].(string) + `"}}`))
	}))
	defer server.Close()

	t.Setenv("GITHUB_TOKEN", "test-token-123")

	src := `
mcp_server GitHub {
    transport: "http"
    url: "` + server.URL + `"
    headers: {
        "Authorization": "Bearer " + env("GITHUB_TOKEN")
    }
}
` + wrapStep(`
        var result = GitHub.call("search_repositories", { query: "org:mh-language mhl" })
        log("total_count: ${result.total_count}")
        log("query: ${result.query}")
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if gotMethod != "tools/call" {
		t.Errorf("server saw method %q, want %q", gotMethod, "tools/call")
	}
	if gotToolName != "search_repositories" {
		t.Errorf("server saw tool name %q, want %q", gotToolName, "search_repositories")
	}
	if gotArgs["query"] != "org:mh-language mhl" {
		t.Errorf("server saw arguments %v, missing expected query", gotArgs)
	}
	if gotAuth != "Bearer test-token-123" {
		t.Errorf("server saw Authorization %q, want %q (env(GITHUB_TOKEN) not resolved into the header)", gotAuth, "Bearer test-token-123")
	}
	if !strings.Contains(out, "total_count: 2\n") {
		t.Errorf("output missing decoded result field, got: %s", out)
	}
	if !strings.Contains(out, "query: org:mh-language mhl\n") {
		t.Errorf("output missing decoded result field, got: %s", out)
	}
}

// TestMCPServerCallFailsClosedOnMissingCredential proves the fail-closed
// behavior BuildRegistryWithError already implements: a header referencing
// an env var that isn't set aborts the call with an error, rather than
// silently sending a request with an empty Authorization header.
func TestMCPServerCallFailsClosedOnMissingCredential(t *testing.T) {
	src := `
mcp_server GitHub {
    transport: "http"
    url: "http://127.0.0.1:1"
    headers: {
        "Authorization": "Bearer " + env("GITHUB_TOKEN_DEFINITELY_UNSET")
    }
}
` + wrapStep(`
        var result = GitHub.call("search_repositories", { query: "x" })
    `)

	_, err := run(t, src)
	if err == nil {
		t.Fatal("expected an error for an unresolved GITHUB_TOKEN_DEFINITELY_UNSET credential, got nil")
	}
}

// TestMCPServerCallOverStdio proves the stdio transport path: a mock
// server process (a one-line shell script standing in for a real MCP
// stdio server) receives the JSON-RPC request on stdin and its stdout is
// decoded back into the call's result.
func TestMCPServerCallOverStdio(t *testing.T) {
	src := `
mcp_server MockTools {
    transport: "stdio"
    command: "sh"
    args: ["-c", "read _; printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"pong\":true}}'"]
}
` + wrapStep(`
        var result = MockTools.call("ping", {})
        log("pong: ${result.pong}")
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "pong: true\n") {
		t.Errorf("output missing decoded stdio result, got: %s", out)
	}
}

// TestMCPServerListTools proves `<Server>.list_tools()` issues a JSON-RPC
// "tools/list" (not "tools/call") and decodes the server's advertised tool
// catalog back into an ordinary MHL value.
func TestMCPServerListTools(t *testing.T) {
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		gotMethod = req.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"search_repositories"},{"name":"get_me"}]}}`))
	}))
	defer server.Close()

	src := `
mcp_server GitHub {
    transport: "http"
    url: "` + server.URL + `"
}
` + wrapStep(`
        var result = GitHub.list_tools()
        log("tool count: ${result.tools.size()}")
        log("first tool: ${result.tools.get_index(0).name}")
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotMethod != "tools/list" {
		t.Errorf("server saw method %q, want %q", gotMethod, "tools/list")
	}
	if !strings.Contains(out, "tool count: 2\n") {
		t.Errorf("output missing decoded tool count, got: %s", out)
	}
	if !strings.Contains(out, "first tool: search_repositories\n") {
		t.Errorf("output missing decoded first tool name, got: %s", out)
	}
}

// TestMCPServerDiscover proves `<Server>.discover()` issues a JSON-RPC
// "server/discover" and decodes the server's advertised versions/identity
// back into an ordinary MHL value.
func TestMCPServerDiscover(t *testing.T) {
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		gotMethod = req.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"supported":["2026-07-28"],"name":"GitHub MCP"}}`))
	}))
	defer server.Close()

	src := `
mcp_server GitHub {
    transport: "http"
    url: "` + server.URL + `"
}
` + wrapStep(`
        var info = GitHub.discover()
        log("supported: ${info.supported}")
        log("name: ${info.name}")
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotMethod != "server/discover" {
		t.Errorf("server saw method %q, want %q", gotMethod, "server/discover")
	}
	if !strings.Contains(out, "name: GitHub MCP\n") {
		t.Errorf("output missing decoded discover result, got: %s", out)
	}
}

// TestMCPServerCallRejectsInputRequiredResult proves the wiring end to end:
// a server responding with an "input_required" resultType aborts `.call()`
// with a clear error naming the limitation, not a decoded
// InputRequiredResult masquerading as the tool's real data.
func TestMCPServerCallRejectsInputRequiredResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"resultType":"input_required","inputRequests":[]}}`))
	}))
	defer server.Close()

	src := `
mcp_server GitHub {
    transport: "http"
    url: "` + server.URL + `"
}
` + wrapStep(`
        var result = GitHub.call("book_flight", {})
    `)

	_, err := run(t, src)
	if err == nil {
		t.Fatal("expected an error for an input_required result, got nil")
	}
	if !strings.Contains(err.Error(), "Multi Round-Trip") {
		t.Errorf("error should explain the MRTR limitation, got: %v", err)
	}
}

// TestMCPServerCallMirrorsXMCPHeaderAnnotatedArguments proves x-mcp-header
// support end to end: `.call()` first probes "tools/list" for the target
// tool's inputSchema, finds its `x-mcp-header`-annotated property, and
// mirrors that argument's value into the matching "Mcp-Param-{Name}" header
// on the actual "tools/call" request — an argument NOT annotated stays out
// of the headers entirely.
func TestMCPServerCallMirrorsXMCPHeaderAnnotatedArguments(t *testing.T) {
	var gotRegionHeader string
	var sawParamHeaderForQuery bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "tools/list" {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[
				{"name":"execute_sql","inputSchema":{"type":"object","properties":{
					"region":{"type":"string","x-mcp-header":"Region"},
					"query":{"type":"string"}
				}}}
			]}}`))
			return
		}
		gotRegionHeader = r.Header.Get("Mcp-Param-Region")
		sawParamHeaderForQuery = r.Header.Get("Mcp-Param-Query") != ""
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer server.Close()

	src := `
mcp_server SpannerDB {
    transport: "http"
    url: "` + server.URL + `"
}
` + wrapStep(`
        var result = SpannerDB.call("execute_sql", { region: "us-west1", query: "SELECT 1" })
    `)

	_, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotRegionHeader != "us-west1" {
		t.Errorf("Mcp-Param-Region = %q, want %q", gotRegionHeader, "us-west1")
	}
	if sawParamHeaderForQuery {
		t.Error("Mcp-Param-Query should not be set: query has no x-mcp-header annotation")
	}
}
