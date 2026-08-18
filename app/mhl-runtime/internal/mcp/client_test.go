package mcp_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/mcp"
)

// TestMain lets the test binary re-exec itself as a mock MCP stdio server when
// MHL_MCP_STDIO_MOCK is set. This gives the stdio transport a real subprocess
// to talk to, cross-platform, without external scripts.
func TestMain(m *testing.M) {
	if os.Getenv("MHL_MCP_STDIO_MOCK") != "" {
		runStdioMock()
		return
	}
	os.Exit(m.Run())
}

// runStdioMock reads a single JSON-RPC request line from stdin and writes a
// canned JSON-RPC result (with a `_meta.ttlMs`) to stdout. It holds no state:
// every invocation behaves identically, proving statelessness across calls.
func runStdioMock() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if !sc.Scan() {
		os.Exit(0)
	}
	var req mcp.Request
	_ = json.Unmarshal(sc.Bytes(), &req)
	resp := mcp.Response{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      req.ID,
		Result:  json.RawMessage(fmt.Sprintf(`{"echo":%q,"ok":true}`, req.Method)),
		Meta:    &mcp.Meta{TTLMs: 1500},
	}
	out, _ := json.Marshal(resp)
	fmt.Fprintln(os.Stdout, string(out))
	os.Exit(0)
}

// AC-4: two sequential stdio tool calls each succeed independently over
// JSON-RPC 2.0 with no session state carried over.
func TestCallToolStdioStateless(t *testing.T) {
	server := mcp.ServerConfig{
		Name:      "PostgresDB",
		Transport: mcp.TransportStdio,
		Command:   os.Args[0], // re-exec this test binary as the mock
		Args:      []string{"-test.run=TestMain"},
	}
	// The mock is activated purely by the env var; set it for child processes.
	t.Setenv("MHL_MCP_STDIO_MOCK", "1")

	client := mcp.NewClient()
	for i := 0; i < 2; i++ {
		res, err := client.CallTool(server, mcp.ToolRequest{
			Method: "tools/call",
			Params: map[string]interface{}{"name": "query", "arguments": map[string]interface{}{"sql": "SELECT 1"}},
		})
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		if res.TTLMs != 1500 {
			t.Errorf("call %d: ttlMs = %d, want 1500", i, res.TTLMs)
		}
		var payload struct {
			Echo string `json:"echo"`
			OK   bool   `json:"ok"`
		}
		if err := json.Unmarshal(res.Raw, &payload); err != nil {
			t.Fatalf("call %d: decode result: %v", i, err)
		}
		if payload.Echo != "tools/call" || !payload.OK {
			t.Errorf("call %d: unexpected result %+v", i, payload)
		}
	}
}

func TestCallToolStdioProcessFailure(t *testing.T) {
	server := mcp.ServerConfig{
		Name:      "Broken",
		Transport: mcp.TransportStdio,
		Command:   "definitely-not-a-real-command-xyz",
	}
	client := mcp.NewClient()
	_, err := client.CallTool(server, mcp.ToolRequest{Method: "tools/call"})
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	var mcpErr *mcp.MCPError
	if !asMCPError(err, &mcpErr) {
		t.Fatalf("expected *mcp.MCPError, got %T: %v", err, err)
	}
	if mcpErr.Transport != "stdio" {
		t.Errorf("transport = %q, want stdio", mcpErr.Transport)
	}
}

// AC-5: an HTTP mcp_server call is sent statelessly with the resolved bearer
// token in the Authorization header and the response is correctly decoded.
func TestCallToolHTTPBearer(t *testing.T) {
	var gotAuth string
	var gotBody mcp.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		resp := mcp.Response{
			JSONRPC: mcp.JSONRPCVersion,
			ID:      gotBody.ID,
			Result:  json.RawMessage(`{"stars":42}`),
			Meta:    &mcp.Meta{TTLMs: 60000},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	server := mcp.ServerConfig{
		Name:      "GitHubServer",
		Transport: mcp.TransportHTTP,
		URL:       srv.URL,
		Headers:   map[string]string{"Authorization": "Bearer ghp_test_token"},
	}

	client := mcp.NewClient()
	res, err := client.CallTool(server, mcp.ToolRequest{Method: "tools/call"})
	if err != nil {
		t.Fatalf("http call failed: %v", err)
	}
	if gotAuth != "Bearer ghp_test_token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer ghp_test_token")
	}
	if gotBody.JSONRPC != mcp.JSONRPCVersion {
		t.Errorf("request jsonrpc = %q, want %q", gotBody.JSONRPC, mcp.JSONRPCVersion)
	}
	if res.TTLMs != 60000 {
		t.Errorf("ttlMs = %d, want 60000", res.TTLMs)
	}
	var payload struct {
		Stars int `json:"stars"`
	}
	if err := json.Unmarshal(res.Raw, &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if payload.Stars != 42 {
		t.Errorf("stars = %d, want 42", payload.Stars)
	}
}

func TestCallToolHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	server := mcp.ServerConfig{Name: "GitHubServer", Transport: mcp.TransportHTTP, URL: srv.URL}
	client := mcp.NewClient()
	_, err := client.CallTool(server, mcp.ToolRequest{Method: "tools/call"})
	if err == nil {
		t.Fatal("expected an HTTP error, got nil")
	}
	var mcpErr *mcp.MCPError
	if !asMCPError(err, &mcpErr) {
		t.Fatalf("expected *mcp.MCPError, got %T", err)
	}
	if mcpErr.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", mcpErr.Code)
	}
}

func TestCallToolUnsupportedTransport(t *testing.T) {
	server := mcp.ServerConfig{Name: "X", Transport: mcp.Transport("carrier-pigeon")}
	client := mcp.NewClient()
	if _, err := client.CallTool(server, mcp.ToolRequest{Method: "tools/call"}); err == nil {
		t.Fatal("expected error for unsupported transport")
	}
}

// asMCPError is a tiny errors.As helper that avoids importing errors in each
// test while keeping the intent clear.
func asMCPError(err error, target **mcp.MCPError) bool {
	for err != nil {
		if e, ok := err.(*mcp.MCPError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
			continue
		}
		break
	}
	return false
}
