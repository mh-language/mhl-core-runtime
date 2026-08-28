package mcp_test

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/mcp"
)

// TestMain lets the test binary re-exec itself as a mock MCP stdio server when
// MHL_MCP_STDIO_MOCK is set. This gives the stdio transport a real subprocess
// to talk to, cross-platform, without external scripts. MHL_MCP_STDIO_MOCK_MODE
// selects the wire behavior:
//
//	""              one stateless line in, one canned line out, exit
//	"handshake"     full initialize/initialized/call sequence, exit
//	"auto"          initialize -> handshake; else reject a _meta call -32602; else echo
//	"auto-linger"   like "auto" but sleeps after the -32602 (proves the probe
//	                does not wait for the process to exit)
//	"initialize-hang" read one line, never respond (initialize-timeout test)
//	"reject-all"    answer every first line with -32602 (both attempts fail)
func TestMain(m *testing.M) {
	if os.Getenv("MHL_MCP_STDIO_MOCK") != "" {
		runStdioMock()
		return
	}
	os.Exit(m.Run())
}

func runStdioMock() {
	switch os.Getenv("MHL_MCP_STDIO_MOCK_MODE") {
	case "handshake":
		runStdioHandshakeMock(true)
	case "auto":
		runStdioAutoMock(false)
	case "auto-linger":
		runStdioAutoMock(true)
	case "initialize-hang":
		br := bufio.NewReader(os.Stdin)
		_, _ = br.ReadBytes('\n')
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "reject-all":
		br := bufio.NewReader(os.Stdin)
		line, _ := br.ReadBytes('\n')
		var req mcp.Request
		_ = json.Unmarshal(line, &req)
		mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: req.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "Invalid request parameters"}})
		os.Exit(0)
	default:
		runStdioStatelessMock()
	}
}

// runStdioStatelessMock reads a single JSON-RPC request line from stdin and
// writes a canned JSON-RPC result (with a `_meta.ttlMs`) to stdout. It holds no
// state: every invocation behaves identically, proving statelessness across
// calls.
func runStdioStatelessMock() {
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

func mockWriteJSON(v any) {
	out, _ := json.Marshal(v)
	fmt.Fprintln(os.Stdout, string(out))
}

func mockParamsMap(raw json.RawMessage) map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &m)
	return m
}

// runStdioHandshakeMock serves initialize -> initialized -> one real call. When
// strict, it fails loudly if the first line is not an `initialize`.
func runStdioHandshakeMock(strict bool) {
	br := bufio.NewReader(os.Stdin)

	line, err := br.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		os.Exit(0)
	}
	var initReq mcp.Request
	_ = json.Unmarshal(line, &initReq)
	if initReq.Method != "initialize" {
		if strict {
			mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: initReq.ID,
				Error: &mcp.RPCError{Code: -32600, Message: "expected initialize, got " + initReq.Method}})
			os.Exit(1)
		}
		return
	}
	var initParams struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(initReq.Params, &initParams)
	if initParams.ProtocolVersion == "" {
		mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: initReq.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "initialize missing protocolVersion"}})
		os.Exit(1)
	}
	mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: initReq.ID,
		Result: json.RawMessage(fmt.Sprintf(
			`{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"mock","version":"0"}}`,
			initParams.ProtocolVersion))})

	// initialized notification — consumed, no reply.
	if _, err := br.ReadBytes('\n'); err != nil {
		os.Exit(0)
	}

	line, err = br.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		os.Exit(0)
	}
	var realReq mcp.Request
	_ = json.Unmarshal(line, &realReq)
	params := mockParamsMap(realReq.Params)
	if _, hasMeta := params["_meta"]; hasMeta {
		mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: realReq.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "handshake call must not carry _meta"}})
		os.Exit(1)
	}
	result := map[string]any{"echo": realReq.Method, "ok": true, "initProtocolVersion": initParams.ProtocolVersion}
	if realReq.Params != nil {
		result["params"] = json.RawMessage(realReq.Params)
	}
	rb, _ := json.Marshal(result)
	mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: realReq.ID, Result: rb})
	os.Exit(0)
}

// runStdioAutoMock rejects a stateless _meta-bearing call with -32602 (like a
// real handshake server), but serves the handshake when the first line is an
// `initialize`. With linger it sleeps after the rejection instead of exiting.
func runStdioAutoMock(linger bool) {
	br := bufio.NewReader(os.Stdin)
	line, err := br.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		os.Exit(0)
	}
	var req mcp.Request
	_ = json.Unmarshal(line, &req)

	if req.Method == "initialize" {
		serveHandshakeFrom(br, req)
		return
	}

	params := mockParamsMap(req.Params)
	if _, hasMeta := params["_meta"]; hasMeta {
		mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: req.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "Invalid request parameters"}})
		if linger {
			time.Sleep(30 * time.Second)
		}
		os.Exit(0)
	}
	mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: req.ID,
		Result: json.RawMessage(fmt.Sprintf(`{"echo":%q,"ok":true}`, req.Method))})
	os.Exit(0)
}

// serveHandshakeFrom finishes a handshake whose `initialize` line was already
// read into initReq.
func serveHandshakeFrom(br *bufio.Reader, initReq mcp.Request) {
	var initParams struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(initReq.Params, &initParams)
	mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: initReq.ID,
		Result: json.RawMessage(fmt.Sprintf(
			`{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"mock"}}`,
			initParams.ProtocolVersion))})
	if _, err := br.ReadBytes('\n'); err != nil {
		os.Exit(0)
	}
	line, err := br.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		os.Exit(0)
	}
	var realReq mcp.Request
	_ = json.Unmarshal(line, &realReq)
	params := mockParamsMap(realReq.Params)
	if _, hasMeta := params["_meta"]; hasMeta {
		mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: realReq.ID,
			Error: &mcp.RPCError{Code: -32602, Message: "handshake call must not carry _meta"}})
		os.Exit(1)
	}
	result := map[string]any{"echo": realReq.Method, "ok": true, "initProtocolVersion": initParams.ProtocolVersion}
	if realReq.Params != nil {
		result["params"] = json.RawMessage(realReq.Params)
	}
	rb, _ := json.Marshal(result)
	mockWriteJSON(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: realReq.ID, Result: rb})
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

// --- protocol negotiation --------------------------------------------------

func stdioMockServer(t *testing.T, mode string) mcp.ServerConfig {
	t.Helper()
	t.Setenv("MHL_MCP_STDIO_MOCK", "1")
	t.Setenv("MHL_MCP_STDIO_MOCK_MODE", mode)
	return mcp.ServerConfig{
		Name:      "Mock",
		Transport: mcp.TransportStdio,
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestMain"},
	}
}

func TestParseProtocol(t *testing.T) {
	ok := []struct {
		in   string
		want mcp.Protocol
	}{
		{"", mcp.ProtocolAuto},
		{"auto", mcp.ProtocolAuto},
		{"2026-07-28", mcp.ProtocolStateless},
		{"2025-11-25", mcp.ProtocolHandshake2511},
		{"2025-06-18", mcp.ProtocolHandshake2506},
		{"  2025-06-18 ", mcp.ProtocolHandshake2506},
	}
	for _, tc := range ok {
		got, err := mcp.ParseProtocol(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseProtocol(%q) = %q, %v; want %q, nil", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"modern", "legacy", "2024-11-05", "bogus"} {
		if _, err := mcp.ParseProtocol(bad); err == nil {
			t.Errorf("ParseProtocol(%q): want error", bad)
		}
	}
}

func TestCallToolStdioStatelessUnchanged(t *testing.T) {
	server := stdioMockServer(t, "") // default stateless mock
	for _, proto := range []mcp.Protocol{"", mcp.ProtocolStateless} {
		server.Protocol = proto
		res, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
			Method: "tools/call",
			Params: map[string]interface{}{"name": "q"},
		})
		if err != nil {
			t.Fatalf("proto %q: %v", proto, err)
		}
		if res.TTLMs != 1500 {
			t.Errorf("proto %q: ttlMs=%d want 1500", proto, res.TTLMs)
		}
		var payload struct {
			Echo string `json:"echo"`
		}
		if err := json.Unmarshal(res.Raw, &payload); err != nil || payload.Echo != "tools/call" {
			t.Errorf("proto %q: unexpected result %s (%v)", proto, res.Raw, err)
		}
	}
}

func TestCallToolStdioHandshakePinned(t *testing.T) {
	for _, tc := range []struct {
		proto   mcp.Protocol
		version string
	}{
		{mcp.ProtocolHandshake2511, "2025-11-25"},
		{mcp.ProtocolHandshake2506, "2025-06-18"},
	} {
		server := stdioMockServer(t, "handshake")
		server.Protocol = tc.proto
		res, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
			Method: "tools/call",
			Params: map[string]interface{}{
				"name":      "get_article",
				"arguments": map[string]any{"title": "MCP"},
			},
		})
		if err != nil {
			t.Fatalf("proto %q: %v", tc.proto, err)
		}
		var payload struct {
			Echo                string `json:"echo"`
			InitProtocolVersion string `json:"initProtocolVersion"`
			Params              struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(res.Raw, &payload); err != nil {
			t.Fatalf("proto %q: decode %v", tc.proto, err)
		}
		if payload.Echo != "tools/call" {
			t.Errorf("proto %q: echo=%q", tc.proto, payload.Echo)
		}
		if payload.InitProtocolVersion != tc.version {
			t.Errorf("proto %q: initialize advertised %q, want %q", tc.proto, payload.InitProtocolVersion, tc.version)
		}
		if payload.Params.Name != "get_article" || payload.Params.Arguments["title"] != "MCP" {
			t.Errorf("proto %q: arguments not forwarded: %+v", tc.proto, payload.Params)
		}
	}
}

func TestCallToolStdioAutoFallsBackAfterInvalidParams(t *testing.T) {
	server := stdioMockServer(t, "auto") // Protocol "" -> auto
	res, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "get_article"},
	})
	if err != nil {
		t.Fatalf("expected handshake fallback to succeed: %v", err)
	}
	var payload struct {
		Echo                string `json:"echo"`
		InitProtocolVersion string `json:"initProtocolVersion"`
	}
	if err := json.Unmarshal(res.Raw, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Echo != "tools/call" {
		t.Errorf("echo=%q", payload.Echo)
	}
	if payload.InitProtocolVersion != "2025-11-25" {
		t.Errorf("auto fallback should advertise 2025-11-25, got %q", payload.InitProtocolVersion)
	}
}

func TestCallToolStdioExplicitStatelessDoesNotFallBack(t *testing.T) {
	server := stdioMockServer(t, "auto")
	server.Protocol = mcp.ProtocolStateless
	_, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "x"},
	})
	if err == nil {
		t.Fatal("want the raw -32602 to surface")
	}
	var me *mcp.MCPError
	if !asMCPError(err, &me) || me.Code != -32602 {
		t.Fatalf("want MCPError code -32602, got %v", err)
	}
}

func TestCallToolStdioAutoProbeDoesNotWaitForExit(t *testing.T) {
	server := stdioMockServer(t, "auto-linger")
	start := time.Now()
	res, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "x"},
	})
	if err != nil {
		t.Fatalf("auto should fall back despite the stateless server lingering: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("probe waited for the lingering process (%s)", elapsed)
	}
	var payload struct {
		Echo string `json:"echo"`
	}
	if err := json.Unmarshal(res.Raw, &payload); err != nil || payload.Echo != "tools/call" {
		t.Errorf("unexpected result %s (%v)", res.Raw, err)
	}
}

func TestCallToolStdioHandshakeInitializeTimeout(t *testing.T) {
	server := stdioMockServer(t, "initialize-hang")
	server.Protocol = mcp.ProtocolHandshake2511
	c := mcp.NewClient()
	c.Timeout = 1 * time.Second
	start := time.Now()
	_, err := c.CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "x"},
	})
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("did not honor Timeout (%s)", elapsed)
	}
	var me *mcp.MCPError
	if !asMCPError(err, &me) || me.Transport != "stdio" {
		t.Fatalf("want stdio MCPError, got %v", err)
	}
}

func TestCallToolStdioListToolsAndDiscoverHandshake(t *testing.T) {
	t.Run("tools/list", func(t *testing.T) {
		server := stdioMockServer(t, "handshake")
		server.Protocol = mcp.ProtocolHandshake2511
		res, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{Method: "tools/list"})
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Echo string `json:"echo"`
		}
		if err := json.Unmarshal(res.Raw, &payload); err != nil || payload.Echo != "tools/list" {
			t.Errorf("unexpected result %s (%v)", res.Raw, err)
		}
	})
	t.Run("server/discover", func(t *testing.T) {
		server := stdioMockServer(t, "handshake")
		server.Protocol = mcp.ProtocolHandshake2511
		res, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{Method: "server/discover"})
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			ProtocolVersion string                 `json:"protocolVersion"`
			ServerInfo      map[string]interface{} `json:"serverInfo"`
		}
		if err := json.Unmarshal(res.Raw, &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload.ProtocolVersion != "2025-11-25" || payload.ServerInfo["name"] != "mock" {
			t.Errorf("discover should surface the initialize result: %+v", payload)
		}
	})
}

func TestCallToolAutoBothProtocolsFailNamesBothStdio(t *testing.T) {
	server := stdioMockServer(t, "reject-all") // -32602 to both the probe and initialize
	_, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "x"},
	})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "no compatible MCP protocol") ||
		!strings.Contains(err.Error(), "2026-07-28") || !strings.Contains(err.Error(), "2025-11-25") {
		t.Errorf("combined error must name both protocols: %v", err)
	}
}

// --- HTTP handshake ------------------------------------------------------

type handshakeCapture struct {
	requests            int
	initAdvertised      string
	initClientVersion   string
	initializedSession  string
	realSession         string
	realProtoHeader     string
	realMcpMethodHeader string
	realBodyHadMeta     bool
}

func newHandshakeHTTPServer(t *testing.T, answerVersion string, rejectMeta bool) (*httptest.Server, *handshakeCapture) {
	t.Helper()
	capture := &handshakeCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if rejectMeta && strings.Contains(string(body), `"_meta"`) {
			http.Error(w, "meta not accepted", http.StatusBadRequest)
			return
		}
		capture.requests++
		var msg mcp.Request
		_ = json.Unmarshal(body, &msg)

		switch msg.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
				ClientInfo      struct {
					Version string `json:"version"`
				} `json:"clientInfo"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			capture.initAdvertised = p.ProtocolVersion
			capture.initClientVersion = p.ClientInfo.Version
			ver := p.ProtocolVersion
			if answerVersion != "" {
				ver = answerVersion
			}
			w.Header().Set("Mcp-Session-Id", "sess-123")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: msg.ID,
				Result: json.RawMessage(fmt.Sprintf(`{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"mock"}}`, ver))})
		case "notifications/initialized":
			capture.initializedSession = r.Header.Get("Mcp-Session-Id")
			w.WriteHeader(http.StatusAccepted)
		default:
			capture.realSession = r.Header.Get("Mcp-Session-Id")
			capture.realProtoHeader = r.Header.Get("MCP-Protocol-Version")
			capture.realMcpMethodHeader = r.Header.Get("Mcp-Method")
			capture.realBodyHadMeta = strings.Contains(string(body), `"_meta"`)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: msg.ID,
				Result: json.RawMessage(fmt.Sprintf(`{"echo":%q,"ok":true}`, msg.Method))})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func TestCallToolHTTPHandshake(t *testing.T) {
	srv, capture := newHandshakeHTTPServer(t, "", false)
	server := mcp.ServerConfig{Name: "Wiki", Transport: mcp.TransportHTTP, URL: srv.URL, Protocol: mcp.ProtocolHandshake2511}
	res, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "get_article"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if capture.requests != 3 {
		t.Errorf("requests=%d want 3 (initialize, initialized, call)", capture.requests)
	}
	if capture.initAdvertised != "2025-11-25" {
		t.Errorf("initialize advertised %q, want 2025-11-25", capture.initAdvertised)
	}
	if capture.initClientVersion == "" {
		t.Error("initialize clientInfo.version must be non-empty — some servers reject an empty version")
	}
	if capture.initializedSession != "sess-123" || capture.realSession != "sess-123" {
		t.Errorf("session id not echoed: initialized=%q real=%q", capture.initializedSession, capture.realSession)
	}
	if capture.realProtoHeader != "2025-11-25" {
		t.Errorf("MCP-Protocol-Version on the real call = %q, want 2025-11-25", capture.realProtoHeader)
	}
	if capture.realMcpMethodHeader != "" {
		t.Errorf("Mcp-Method must not be sent in handshake mode, got %q", capture.realMcpMethodHeader)
	}
	if capture.realBodyHadMeta {
		t.Error("the real call body must not carry _meta")
	}
	var payload struct {
		Echo string `json:"echo"`
	}
	if err := json.Unmarshal(res.Raw, &payload); err != nil || payload.Echo != "tools/call" {
		t.Errorf("unexpected result %s (%v)", res.Raw, err)
	}
}

func TestCallToolHTTPHandshakeNegotiatesVersionDown(t *testing.T) {
	srv, capture := newHandshakeHTTPServer(t, "2025-06-18", false)
	server := mcp.ServerConfig{Name: "Wiki", Transport: mcp.TransportHTTP, URL: srv.URL, Protocol: mcp.ProtocolHandshake2511}
	if _, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	if capture.initAdvertised != "2025-11-25" {
		t.Errorf("advertised %q, want 2025-11-25", capture.initAdvertised)
	}
	if capture.realProtoHeader != "2025-06-18" {
		t.Errorf("MCP-Protocol-Version should echo the negotiated 2025-06-18, got %q", capture.realProtoHeader)
	}
}

func TestCallToolHTTPAutoStatelessServerSendsNoInitialize(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		body, _ := io.ReadAll(r.Body)
		var msg mcp.Request
		_ = json.Unmarshal(body, &msg)
		if msg.Method == "initialize" {
			t.Errorf("a stateless server should never receive initialize")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcp.Response{JSONRPC: mcp.JSONRPCVersion, ID: msg.ID, Result: json.RawMessage(`{"ok":true}`)})
	}))
	defer srv.Close()
	server := mcp.ServerConfig{Name: "S", Transport: mcp.TransportHTTP, URL: srv.URL}
	if _, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("requests=%d want 1", count)
	}
}

func TestCallToolHTTPAutoFallsBackAfter400(t *testing.T) {
	srv, capture := newHandshakeHTTPServer(t, "", true)
	server := mcp.ServerConfig{Name: "Wiki", Transport: mcp.TransportHTTP, URL: srv.URL} // auto
	res, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "x"},
	})
	if err != nil {
		t.Fatalf("auto should fall back to the handshake: %v", err)
	}
	if capture.initAdvertised != "2025-11-25" {
		t.Errorf("fallback advertised %q, want 2025-11-25", capture.initAdvertised)
	}
	var payload struct {
		Echo string `json:"echo"`
	}
	if err := json.Unmarshal(res.Raw, &payload); err != nil || payload.Echo != "tools/call" {
		t.Errorf("unexpected result %s (%v)", res.Raw, err)
	}
}

func TestCallToolAutoBothProtocolsFailNamesBothHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer srv.Close()
	server := mcp.ServerConfig{Name: "Bad", Transport: mcp.TransportHTTP, URL: srv.URL}
	_, err := mcp.NewClient().CallTool(server, mcp.ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "x"},
	})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "no compatible MCP protocol") ||
		!strings.Contains(err.Error(), "2026-07-28") || !strings.Contains(err.Error(), "2025-11-25") {
		t.Errorf("error must name both protocols: %v", err)
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
