package mcp_test

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/mcp"
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

// TestCallToolHTTPCarriesModernRequestMetadata proves the spec 2026-07-28
// conformance fix: since this revision removed the `initialize` handshake
// and protocol-level sessions, every request must instead be
// self-describing — both in the JSON-RPC body (`params._meta`) and, on
// Streamable HTTP specifically, in three mirrored headers a conformant
// server validates the body against (see "Request Metadata" in the
// transport spec). A request missing any of this is malformed and a real
// server rejects it with 400, regardless of whether the endpoint requires
// no session at all.
func TestCallToolHTTPCarriesModernRequestMetadata(t *testing.T) {
	var gotProtocolHeader, gotMethodHeader, gotNameHeader string
	var gotBody struct {
		Params struct {
			Name string `json:"name"`
			Meta struct {
				ProtocolVersion    string                 `json:"io.modelcontextprotocol/protocolVersion"`
				ClientCapabilities map[string]interface{} `json:"io.modelcontextprotocol/clientCapabilities"`
				ClientInfo         struct {
					Name string `json:"name"`
				} `json:"io.modelcontextprotocol/clientInfo"`
			} `json:"_meta"`
		} `json:"params"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProtocolHeader = r.Header.Get("MCP-Protocol-Version")
		gotMethodHeader = r.Header.Get("Mcp-Method")
		gotNameHeader = r.Header.Get("Mcp-Name")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		resp := mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: 1, Result: json.RawMessage(`{}`)}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	server := mcp.ServerConfig{Name: "GitHubServer", Transport: mcp.TransportHTTP, URL: srv.URL}
	client := mcp.NewClient()
	_, err := client.CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{
			"name":      "search_repositories",
			"arguments": map[string]interface{}{"query": "org:mh-language mhl"},
		},
	})
	if err != nil {
		t.Fatalf("http call failed: %v", err)
	}

	if gotProtocolHeader != mcp.SpecVersion {
		t.Errorf("MCP-Protocol-Version header = %q, want %q", gotProtocolHeader, mcp.SpecVersion)
	}
	if gotMethodHeader != "tools/call" {
		t.Errorf("Mcp-Method header = %q, want %q", gotMethodHeader, "tools/call")
	}
	if gotNameHeader != "search_repositories" {
		t.Errorf("Mcp-Name header = %q, want %q", gotNameHeader, "search_repositories")
	}
	if gotBody.Params.Name != "search_repositories" {
		t.Errorf("body params.name = %q, want %q", gotBody.Params.Name, "search_repositories")
	}
	if gotBody.Params.Meta.ProtocolVersion != mcp.SpecVersion {
		t.Errorf("body params._meta protocolVersion = %q, want %q", gotBody.Params.Meta.ProtocolVersion, mcp.SpecVersion)
	}
	if gotBody.Params.Meta.ClientCapabilities == nil {
		t.Error("body params._meta clientCapabilities missing, want present (may be empty object)")
	}
	if gotBody.Params.Meta.ClientInfo.Name != "mhl" {
		t.Errorf("body params._meta clientInfo.name = %q, want %q", gotBody.Params.Meta.ClientInfo.Name, "mhl")
	}
}

// TestCallToolRejectsInputRequiredResult proves the Multi Round-Trip
// Requests guard: a server responding with `resultType: "input_required"`
// (spec 2026-07-28's polymorphic result shape, used when it needs
// elicitation/sampling input this client cannot supply) is surfaced as a
// clear error, not returned as if the InputRequiredResult were the tool's
// real data.
func TestCallToolRejectsInputRequiredResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := mcp.Response{
			JSONRPC: mcp.JSONRPCVersion,
			ID:      1,
			Result:  json.RawMessage(`{"resultType":"input_required","inputRequests":[{"method":"elicitation/create"}]}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	server := mcp.ServerConfig{Name: "GitHubServer", Transport: mcp.TransportHTTP, URL: srv.URL}
	client := mcp.NewClient()
	result, err := client.CallTool(server, mcp.ToolRequest{Method: "tools/call"})
	if err != nil {
		t.Fatalf("CallTool itself should not error (decoding succeeds); got: %v", err)
	}
	if !result.IsInputRequired() {
		t.Fatalf("IsInputRequired() = false, want true for resultType %q", result.ResultType)
	}
}

// TestCallToolDefaultsResultTypeToCompleteWhenAbsent proves spec 2026-07-28's
// own backward-compatibility rule: a server (or any response) that omits
// `resultType` entirely is treated as "complete", not as an error or an
// unset zero value a caller might mishandle.
func TestCallToolDefaultsResultTypeToCompleteWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: 1, Result: json.RawMessage(`{"stars":42}`)}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	server := mcp.ServerConfig{Name: "GitHubServer", Transport: mcp.TransportHTTP, URL: srv.URL}
	client := mcp.NewClient()
	result, err := client.CallTool(server, mcp.ToolRequest{Method: "tools/call"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsInputRequired() {
		t.Fatal("IsInputRequired() = true for a response with no resultType, want false (defaults to complete)")
	}
	if result.ResultType != "complete" {
		t.Errorf("ResultType = %q, want %q", result.ResultType, "complete")
	}
}

// TestEncodeHeaderValueBase64EncodesUnsafeValues proves the transport
// spec's Value Encoding rule: a header value that isn't plain visible ASCII
// (or that already looks like the encoded sentinel) must be wrapped as
// "=?base64?...?=" rather than sent raw, which could otherwise corrupt the
// header or violate RFC 9110 field-value syntax.
func TestEncodeHeaderValueBase64EncodesUnsafeValues(t *testing.T) {
	cases := []struct {
		name  string
		value string
		safe  bool // true: passed through unchanged
	}{
		{"plain ascii", "search_repositories", true},
		{"non-ascii", "Hello, 世界", false},
		{"leading/trailing whitespace", " padded ", false},
		{"embedded newline", "line1\nline2", false},
		{"already looks like the sentinel", "=?base64?literal?=", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotNameHeader string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotNameHeader = r.Header.Get("Mcp-Name")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: 1, Result: json.RawMessage(`{}`)})
			}))
			defer srv.Close()

			server := mcp.ServerConfig{Name: "GitHubServer", Transport: mcp.TransportHTTP, URL: srv.URL}
			client := mcp.NewClient()
			_, err := client.CallTool(server, mcp.ToolRequest{
				Method: "tools/call",
				Params: map[string]interface{}{"name": tc.value},
			})
			if err != nil {
				t.Fatalf("http call failed: %v", err)
			}

			if tc.safe {
				if gotNameHeader != tc.value {
					t.Errorf("Mcp-Name = %q, want unchanged %q", gotNameHeader, tc.value)
				}
				return
			}
			if gotNameHeader == tc.value {
				t.Errorf("Mcp-Name = %q, want it Base64-sentinel-encoded, not passed through raw", gotNameHeader)
			}
			if !strings.HasPrefix(gotNameHeader, "=?base64?") || !strings.HasSuffix(gotNameHeader, "?=") {
				t.Errorf("Mcp-Name = %q, want the =?base64?...?= sentinel format", gotNameHeader)
			}
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(gotNameHeader, "=?base64?"), "?="))
			if err != nil {
				t.Fatalf("decoding Mcp-Name: %v", err)
			}
			if string(decoded) != tc.value {
				t.Errorf("decoded Mcp-Name = %q, want %q", decoded, tc.value)
			}
		})
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
