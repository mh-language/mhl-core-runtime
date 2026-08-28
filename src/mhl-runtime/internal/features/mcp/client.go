package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Transport identifies the wire transport of an MCP server.
type Transport string

const (
	// TransportStdio spawns a local process and speaks newline-delimited
	// JSON-RPC over its stdin/stdout.
	TransportStdio Transport = "stdio"
	// TransportHTTP issues a stateless HTTP POST per call, with header-based
	// authentication.
	TransportHTTP Transport = "http"
)

// ServerConfig is the resolved configuration of a declared mcp_server. Any
// credential values (e.g. bearer tokens) are already resolved by the caller;
// this package only consumes resolved values and never stores credentials.
type ServerConfig struct {
	Name      string
	Transport Transport
	// Protocol selects stateless vs handshake behavior. The zero value ("")
	// means ProtocolAuto: stateless first, handshake fallback.
	Protocol Protocol
	// stdio transport
	Command string
	Args    []string
	// http transport
	URL     string
	Headers map[string]string
}

// ToolRequest is a stateless tool invocation. Method is the JSON-RPC method
// (e.g. "tools/call"); Params carries its arguments.
//
// ParamHeaders is x-mcp-header support (spec 2026-07-28's Streamable HTTP
// transport section): raw, not-yet-encoded values to mirror into
// "Mcp-Param-{Name}" headers on the http transport — callHTTP applies the
// same Value Encoding rules (encodeHeaderValue) it already uses for
// Mcp-Name. The caller (mcp_ops.go) is responsible for knowing which
// argument maps to which header name, since that requires the tool's
// inputSchema (from a "tools/list" call) — this package only knows how to
// carry and encode already-decided values, not how to discover them.
// Ignored on the stdio transport, matching the spec's own scoping of
// x-mcp-header to Streamable HTTP.
type ToolRequest struct {
	Method       string
	Params       map[string]interface{}
	ParamHeaders map[string]string
}

// Result is the decoded JSON-RPC result of a tool call, including any `_meta`
// and its `ttlMs` surfaced to the caller (IF-3).
//
// ResultType is spec 2026-07-28's polymorphic result discriminator: servers
// implementing this revision set it to "complete" for an ordinary result or
// "input_required" when the request needs more input via the Multi
// Round-Trip Requests pattern (elicitation/sampling) before it can finish —
// this client doesn't retry with `inputResponses`, so IsInputRequired lets
// a caller (mcp_ops.go) detect that case and fail with a clear message
// instead of returning the InputRequiredResult's shape as if it were the
// tool's real data. A server on an earlier revision that omits the field
// entirely decodes as "complete", per spec's own backward-compatibility
// rule.
type Result struct {
	Raw        json.RawMessage
	Meta       *Meta
	TTLMs      int64
	ResultType string
}

// IsInputRequired reports whether the server responded with an
// "input_required" result — a Multi Round-Trip Requests interim result this
// client cannot continue (see Result's doc comment).
func (r Result) IsInputRequired() bool {
	return r.ResultType == "input_required"
}

// Client is an MCP client. A single Client instance may be reused across
// servers and calls; it holds no per-server session state between calls (a
// handshake session lives only for the duration of one CallTool).
type Client struct {
	// HTTPClient is used for the HTTP transport. If nil, a default client
	// with a sane timeout is used.
	HTTPClient *http.Client
	// Timeout bounds a single stdio call, and one handshake exchange. If zero,
	// a default is used.
	Timeout time.Duration
	// ProbeTimeout bounds the stateless first attempt in ProtocolAuto mode, so
	// a handshake server that ignores the malformed request is detected quickly
	// rather than after the full Timeout. If zero, a default is used.
	ProbeTimeout time.Duration
}

// NewClient returns a Client with default settings.
func NewClient() *Client {
	return &Client{
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		Timeout:      30 * time.Second,
		ProbeTimeout: 5 * time.Second,
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 30 * time.Second
}

func (c *Client) probeTimeout() time.Duration {
	if c.ProbeTimeout > 0 {
		return c.ProbeTimeout
	}
	return 5 * time.Second
}

// CallTool issues one tool call to the given server, negotiating the protocol
// per server.Protocol:
//
//   - ProtocolStateless: the stateless SpecVersion form only (this is the
//     unchanged legacy behavior — a fresh process / fresh request, protocol
//     context in params._meta, no handshake).
//   - ProtocolHandshake2511 / ProtocolHandshake2506: the standard
//     initialize / notifications/initialized handshake, advertising that
//     revision.
//   - ProtocolAuto (the zero value): the stateless form first; on a
//     protocol-incompatibility error (shouldFallBack), a fresh handshake
//     attempt advertising HandshakeVersionLatest. If both fail, the returned
//     error names both attempts.
func (c *Client) CallTool(server ServerConfig, request ToolRequest) (Result, error) {
	switch server.Protocol {
	case ProtocolStateless:
		return c.callStateless(server, request, c.timeout())
	case ProtocolHandshake2511, ProtocolHandshake2506:
		return c.callHandshake(server, request, server.Protocol.advertisedVersion(), c.timeout())
	default: // "", ProtocolAuto, or an unrecognized value (rejected upstream)
		res, err := c.callStatelessProbe(server, request)
		if err == nil {
			return res, nil
		}
		if !shouldFallBack(err) {
			return Result{}, err
		}
		hres, herr := c.callHandshake(server, request, HandshakeVersionLatest, c.timeout())
		if herr == nil {
			return hres, nil
		}
		return Result{}, combinedProtocolError(server, err, herr)
	}
}

// callStateless issues the request in the stateless SpecVersion form.
func (c *Client) callStateless(server ServerConfig, request ToolRequest, timeout time.Duration) (Result, error) {
	switch server.Transport {
	case TransportStdio:
		return c.callStdio(server, request, timeout)
	case TransportHTTP:
		return c.callHTTP(server, request, timeout)
	default:
		return Result{}, &MCPError{
			Server:    server.Name,
			Transport: string(server.Transport),
			Message:   fmt.Sprintf("unsupported transport %q", server.Transport),
		}
	}
}

// callStatelessProbe is the ProtocolAuto first attempt: identical wire bytes to
// callStateless, but bounded by ProbeTimeout and, on stdio, reading the
// response from a live stream instead of waiting for the process to exit — so a
// handshake server's -32602 (or silence) is detected in well under a second.
func (c *Client) callStatelessProbe(server ServerConfig, request ToolRequest) (Result, error) {
	switch server.Transport {
	case TransportStdio:
		return c.callStdioStreaming(server, request, c.probeTimeout())
	case TransportHTTP:
		return c.callHTTP(server, request, c.probeTimeout())
	default:
		return Result{}, &MCPError{
			Server:    server.Name,
			Transport: string(server.Transport),
			Message:   fmt.Sprintf("unsupported transport %q", server.Transport),
		}
	}
}

// callHandshake issues the request over an initialize/initialized session,
// advertising protocolVersion `version` (the server may negotiate it down).
func (c *Client) callHandshake(server ServerConfig, request ToolRequest, version string, timeout time.Duration) (Result, error) {
	switch server.Transport {
	case TransportStdio:
		return c.callHandshakeStdio(server, request, version, timeout)
	case TransportHTTP:
		return c.callHandshakeHTTP(server, request, version, timeout)
	default:
		return Result{}, &MCPError{
			Server:    server.Name,
			Transport: string(server.Transport),
			Message:   fmt.Sprintf("unsupported transport %q", server.Transport),
		}
	}
}

// buildRequest constructs the JSON-RPC request bytes for a tool call.
// Spec 2026-07-28 requires every request to carry its protocol version and
// client capabilities in `params._meta` (RequestMeta) — this applies to
// both transports, not just HTTP, since it is what makes each call
// self-describing now that there is no `initialize` handshake to establish
// that context once per session.
func buildRequest(request ToolRequest) ([]byte, error) {
	params := make(map[string]interface{}, len(request.Params)+1)
	for k, v := range request.Params {
		params[k] = v
	}
	params["_meta"] = RequestMeta{
		ProtocolVersion:    SpecVersion,
		ClientCapabilities: map[string]interface{}{},
		ClientInfo:         mhlClientInfo(),
	}
	p, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      1, // one call per process/request; ids need not be unique across calls
		Method:  request.Method,
		Params:  p,
	}
	return json.Marshal(req)
}

// decodeResponse turns a JSON-RPC response envelope into a Result or a typed
// MCPError. The `_meta.ttlMs` is surfaced from the response envelope, falling
// back to a `_meta` object embedded in the result payload.
func decodeResponse(server ServerConfig, data []byte) (Result, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return Result{}, &MCPError{
			Server:    server.Name,
			Transport: string(server.Transport),
			Message:   "malformed JSON-RPC response",
			Err:       err,
		}
	}
	if resp.Error != nil {
		return Result{}, &MCPError{
			Server:    server.Name,
			Transport: string(server.Transport),
			Code:      resp.Error.Code,
			Message:   resp.Error.Message,
		}
	}

	res := Result{Raw: resp.Result, Meta: resp.Meta, ResultType: "complete"}
	if resp.Meta != nil {
		res.TTLMs = resp.Meta.TTLMs
	}
	// Fall back to a _meta embedded inside the result payload, and read
	// resultType from the same payload — both are fields of the `result`
	// object itself, not the JSON-RPC envelope.
	if len(resp.Result) > 0 {
		var embedded struct {
			Meta       *Meta  `json:"_meta"`
			ResultType string `json:"resultType"`
		}
		if err := json.Unmarshal(resp.Result, &embedded); err == nil {
			if res.TTLMs == 0 && embedded.Meta != nil {
				if res.Meta == nil {
					res.Meta = embedded.Meta
				}
				res.TTLMs = embedded.Meta.TTLMs
			}
			if embedded.ResultType != "" {
				res.ResultType = embedded.ResultType
			}
		}
	}
	return res, nil
}

// callStdio spawns the server process, writes a single JSON-RPC request to its
// stdin, and reads the JSON-RPC response from its stdout. The process is not
// reused, so no session state survives the call.
func (c *Client) callStdio(server ServerConfig, request ToolRequest, timeout time.Duration) (Result, error) {
	if server.Command == "" {
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "no command configured"}
	}
	reqBytes, err := buildRequest(request)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "encoding request", Err: err}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "opening stdin", Err: err}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "starting process", Err: err}
	}

	// Write the request line and close stdin so the server sees EOF.
	if _, err := stdin.Write(append(reqBytes, '\n')); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "writing request", Err: err}
	}
	_ = stdin.Close()

	waitErr := cmd.Wait()

	line, perr := firstJSONLine(stdout.Bytes())
	if perr != nil {
		msg := "reading response"
		if waitErr != nil {
			msg = "process exited without a valid response"
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			msg = msg + ": " + detail
		}
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: msg, Err: firstNonNil(perr, waitErr)}
	}
	return decodeResponse(server, line)
}

// firstJSONLine returns the first non-empty line from a buffer that parses as a
// JSON object, so leading log/diagnostic lines from a server do not break
// decoding.
func firstJSONLine(b []byte) ([]byte, error) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		cp := append([]byte{}, line...)
		if json.Valid(cp) {
			return cp, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no JSON-RPC response line found")
}

// callHTTP issues a single stateless HTTP POST carrying the JSON-RPC request,
// applying the configured headers (e.g. an Authorization bearer token). No
// cookies or session state are retained.
func (c *Client) callHTTP(server ServerConfig, request ToolRequest, timeout time.Duration) (Result, error) {
	if server.URL == "" {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "no url configured"}
	}
	reqBytes, err := buildRequest(request)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "encoding request", Err: err}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(reqBytes))
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "building request", Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	// Spec 2026-07-28's Streamable HTTP transport requires these three
	// headers on every POST, mirroring fields already in the body so
	// intermediaries can route/inspect without parsing JSON — a server
	// MUST reject the request with 400 + HeaderMismatch (-32020) if any is
	// missing or doesn't match the body (see "Request Metadata" in the
	// transport spec). Mcp-Name mirrors `params.name`, which every
	// ToolRequest this client builds sets for a tools/call.
	httpReq.Header.Set("MCP-Protocol-Version", SpecVersion)
	httpReq.Header.Set("Mcp-Method", request.Method)
	if name, ok := request.Params["name"].(string); ok {
		httpReq.Header.Set("Mcp-Name", encodeHeaderValue(name))
	}
	// x-mcp-header support: the caller already decided which arguments map
	// to which header names (mcp_ops.go, from the tool's inputSchema); this
	// is only the encoding step.
	for name, value := range request.ParamHeaders {
		httpReq.Header.Set("Mcp-Param-"+name, encodeHeaderValue(value))
	}
	for k, v := range server.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "request failed", Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "reading response body", Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, &MCPError{
			Server:    server.Name,
			Transport: "http",
			Code:      resp.StatusCode,
			Message:   fmt.Sprintf("HTTP %s", resp.Status),
		}
	}

	// The HTTP/SSE transport may frame the JSON-RPC message as an SSE `data:`
	// event; unwrap it if present, otherwise decode the body directly.
	payload := extractSSEData(body)
	return decodeResponse(server, payload)
}

// extractSSEData returns the JSON payload from an SSE event body (concatenating
// `data:` lines) or the original body if it is not SSE-framed.
func extractSSEData(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return trimmed
	}
	var data bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	found := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			found = true
		}
	}
	if found {
		return data.Bytes()
	}
	return trimmed
}

// headerSentinelPrefix and headerSentinelSuffix mark a Base64-encoded HTTP
// header value per spec 2026-07-28's Value Encoding rules.
const headerSentinelPrefix, headerSentinelSuffix = "=?base64?", "?="

// encodeHeaderValue applies the transport spec's Value Encoding rules to a
// value carried in a mirrored header (here, Mcp-Name): plain visible-ASCII
// text with no leading/trailing whitespace passes through unchanged;
// anything else — non-ASCII bytes, control characters, or leading/trailing
// whitespace, which HTTP header values cannot carry safely — is wrapped as
// "=?base64?<base64 of the UTF-8 bytes>?=". A value that already happens to
// look like that sentinel is also wrapped, so a decoder can never mistake a
// literal value for an encoded one.
func encodeHeaderValue(v string) string {
	if isSafeHeaderValue(v) && !isHeaderSentinel(v) {
		return v
	}
	return headerSentinelPrefix + base64.StdEncoding.EncodeToString([]byte(v)) + headerSentinelSuffix
}

func isHeaderSentinel(v string) bool {
	return strings.HasPrefix(v, headerSentinelPrefix) && strings.HasSuffix(v, headerSentinelSuffix)
}

// isSafeHeaderValue reports whether v can be sent as a raw HTTP header
// value per RFC 9110 §5.5: visible ASCII (0x21-0x7E), space, or horizontal
// tab, with no leading/trailing whitespace.
func isSafeHeaderValue(v string) bool {
	if v == "" {
		return true
	}
	if strings.TrimSpace(v) != v {
		return false
	}
	for _, r := range v {
		if r != ' ' && r != '\t' && (r < 0x21 || r > 0x7E) {
			return false
		}
	}
	return true
}

func firstNonNil(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
