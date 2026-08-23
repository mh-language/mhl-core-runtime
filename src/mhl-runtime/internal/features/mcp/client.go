package mcp

import (
	"bufio"
	"bytes"
	"context"
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
	// stdio transport
	Command string
	Args    []string
	// http transport
	URL     string
	Headers map[string]string
}

// ToolRequest is a stateless tool invocation. Method is the JSON-RPC method
// (e.g. "tools/call"); Params carries its arguments.
type ToolRequest struct {
	Method string
	Params map[string]interface{}
}

// Result is the decoded JSON-RPC result of a tool call, including any `_meta`
// and its `ttlMs` surfaced to the caller (IF-3).
type Result struct {
	Raw   json.RawMessage
	Meta  *Meta
	TTLMs int64
}

// Client is a stateless MCP client. A single Client instance may be reused
// across servers and calls; it holds no per-server session state.
type Client struct {
	// HTTPClient is used for the HTTP transport. If nil, a default client
	// with a sane timeout is used.
	HTTPClient *http.Client
	// Timeout bounds a single stdio call. If zero, a default is used.
	Timeout time.Duration
}

// NewClient returns a Client with default settings.
func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Timeout:    30 * time.Second,
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

// CallTool issues a single stateless tool call to the given server. Each call
// is fully independent: the stdio transport spawns a fresh process and the
// HTTP transport issues a fresh request, with no session/handshake state
// carried over between calls (RF-3, RF-4).
func (c *Client) CallTool(server ServerConfig, request ToolRequest) (Result, error) {
	switch server.Transport {
	case TransportStdio:
		return c.callStdio(server, request)
	case TransportHTTP:
		return c.callHTTP(server, request)
	default:
		return Result{}, &MCPError{
			Server:    server.Name,
			Transport: string(server.Transport),
			Message:   fmt.Sprintf("unsupported transport %q", server.Transport),
		}
	}
}

// buildRequest constructs the JSON-RPC request bytes for a tool call.
func buildRequest(request ToolRequest) ([]byte, error) {
	var params json.RawMessage
	if request.Params != nil {
		p, err := json.Marshal(request.Params)
		if err != nil {
			return nil, err
		}
		params = p
	}
	req := Request{
		JSONRPC: JSONRPCVersion,
		ID:      1, // one call per process/request; ids need not be unique across calls
		Method:  request.Method,
		Params:  params,
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

	res := Result{Raw: resp.Result, Meta: resp.Meta}
	if resp.Meta != nil {
		res.TTLMs = resp.Meta.TTLMs
	}
	// Fall back to a _meta embedded inside the result payload.
	if res.TTLMs == 0 && len(resp.Result) > 0 {
		var embedded struct {
			Meta *Meta `json:"_meta"`
		}
		if err := json.Unmarshal(resp.Result, &embedded); err == nil && embedded.Meta != nil {
			if res.Meta == nil {
				res.Meta = embedded.Meta
			}
			res.TTLMs = embedded.Meta.TTLMs
		}
	}
	return res, nil
}

// callStdio spawns the server process, writes a single JSON-RPC request to its
// stdin, and reads the JSON-RPC response from its stdout. The process is not
// reused, so no session state survives the call.
func (c *Client) callStdio(server ServerConfig, request ToolRequest) (Result, error) {
	if server.Command == "" {
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "no command configured"}
	}
	reqBytes, err := buildRequest(request)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "encoding request", Err: err}
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
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
func (c *Client) callHTTP(server ServerConfig, request ToolRequest) (Result, error) {
	if server.URL == "" {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "no url configured"}
	}
	reqBytes, err := buildRequest(request)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "encoding request", Err: err}
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(reqBytes))
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "building request", Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
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

func firstNonNil(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
