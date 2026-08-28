package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// This file adds the fallback path for MCP servers that speak the standard
// connection lifecycle (revisions 2025-11-25 and 2025-06-18): an `initialize`
// request, a `notifications/initialized` notification, then ordinary calls
// whose `params` carry no `_meta`. Both revisions share this exact flow; the
// only variable is the protocolVersion string advertised in `initialize`,
// which the server may still negotiate down in its response.

// shouldFallBack reports whether a failed stateless attempt looks like a
// protocol incompatibility worth retrying with the handshake. Kept deliberately
// narrow — only the JSON-RPC / HTTP status codes a conformant handshake server
// uses to reject the stateless form's mandatory params._meta — so a genuine
// tool error (bad arguments, method not found), a server crash, a missing
// command, or a network failure is surfaced directly rather than masked by a
// second attempt.
func shouldFallBack(err error) bool {
	var me *MCPError
	if !errors.As(err, &me) {
		return false
	}
	switch me.Code {
	case -32602, -32600, http.StatusBadRequest:
		return true
	default:
		return false
	}
}

// combinedProtocolError is returned when ProtocolAuto tried both the stateless
// form and the handshake and both failed.
func combinedProtocolError(server ServerConfig, statelessErr, handshakeErr error) error {
	return fmt.Errorf(
		"no compatible MCP protocol for mcp_server %q: stateless (%s) attempt: %v; handshake (advertised %s) attempt: %v",
		server.Name, SpecVersion, statelessErr, HandshakeVersionLatest, handshakeErr)
}

// buildBareRequest is buildRequest without the params._meta injection and with
// a caller-supplied id — the form handshake-era servers expect.
func buildBareRequest(request ToolRequest, id int) ([]byte, error) {
	var params json.RawMessage
	if len(request.Params) > 0 {
		p, err := json.Marshal(request.Params)
		if err != nil {
			return nil, err
		}
		params = p
	}
	return json.Marshal(Request{JSONRPC: JSONRPCVersion, ID: id, Method: request.Method, Params: params})
}

func buildInitializeRequest(version string, id int) ([]byte, error) {
	p, err := json.Marshal(handshakeInitializeParams{
		ProtocolVersion: version,
		Capabilities:    map[string]interface{}{},
		ClientInfo:      mhlClientInfo(),
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(Request{JSONRPC: JSONRPCVersion, ID: id, Method: "initialize", Params: p})
}

func buildInitializedNotification() ([]byte, error) {
	return json.Marshal(Notification{JSONRPC: JSONRPCVersion, Method: "notifications/initialized"})
}

// decodeInitializeResult unwraps the `initialize` response envelope.
func decodeInitializeResult(server ServerConfig, transport string, data []byte) (HandshakeInitializeResult, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return HandshakeInitializeResult{}, &MCPError{Server: server.Name, Transport: transport, Message: "malformed initialize response", Err: err}
	}
	if resp.Error != nil {
		return HandshakeInitializeResult{}, &MCPError{Server: server.Name, Transport: transport, Code: resp.Error.Code, Message: "initialize rejected: " + resp.Error.Message}
	}
	var res HandshakeInitializeResult
	if len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, &res); err != nil {
			return HandshakeInitializeResult{}, &MCPError{Server: server.Name, Transport: transport, Message: "malformed initialize result", Err: err}
		}
	}
	return res, nil
}

// initializeResultAsDiscoverResult synthesizes a `.discover()` result from the
// initialize handshake — `server/discover` is a 2026-07-28-only method, so in
// handshake mode the identity/capabilities the caller wants come from here.
func initializeResultAsDiscoverResult(r HandshakeInitializeResult) (Result, error) {
	raw, err := json.Marshal(map[string]interface{}{
		"protocolVersion": r.ProtocolVersion,
		"capabilities":    r.Capabilities,
		"serverInfo":      r.ServerInfo,
		"instructions":    r.Instructions,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Raw: raw, ResultType: "complete"}, nil
}

// readJSONLine reads newline-delimited lines from r until one is a JSON object,
// returning it. Leading non-JSON lines (a server's startup log noise on stdout)
// are skipped, matching firstJSONLine's tolerance.
func readJSONLine(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed) {
			return append([]byte(nil), trimmed...), nil
		}
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("no JSON-RPC response line found")
			}
			return nil, err
		}
	}
}

// stdioSession is a live stdio server process: stdin kept open, stdout read
// incrementally, used for both the ProtocolAuto stateless probe (one write, one
// read) and the full handshake (initialize / initialized / call).
type stdioSession struct {
	server    ServerConfig
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	pr        *io.PipeReader
	stderr    *bytes.Buffer
	ctx       context.Context
	cancel    context.CancelFunc
	waitDone  chan error
	once      sync.Once
	stderrStr string
}

func startStdioSession(server ServerConfig, timeout time.Duration) (*stdioSession, error) {
	if server.Command == "" {
		return nil, &MCPError{Server: server.Name, Transport: "stdio", Message: "no command configured"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	// Bound how long Wait blocks on lingering I/O after ctx fires and the
	// process is signalled (spec §Lifecycle: close stdin, then escalate).
	cmd.WaitDelay = 2 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, &MCPError{Server: server.Name, Transport: "stdio", Message: "opening stdin", Err: err}
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		return nil, &MCPError{Server: server.Name, Transport: "stdio", Message: "starting process", Err: err}
	}

	s := &stdioSession{
		server:   server,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewReaderSize(pr, 64*1024),
		pr:       pr,
		stderr:   &stderr,
		ctx:      ctx,
		cancel:   cancel,
		waitDone: make(chan error, 1),
	}
	go func() {
		werr := cmd.Wait()
		// Unblock any reader on pr now that no more output can arrive.
		_ = pw.CloseWithError(io.EOF)
		s.waitDone <- werr
	}()
	return s, nil
}

func (s *stdioSession) write(b []byte) error {
	_, err := s.stdin.Write(append(b, '\n'))
	return err
}

// readMessage returns the next JSON-RPC line, honoring the session deadline. On
// timeout it returns ctx.Err(); the caller then shuts the session down, which
// unblocks the pending read goroutine (its channel is buffered, so it cannot
// leak).
func (s *stdioSession) readMessage() ([]byte, error) {
	type result struct {
		line []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := readJSONLine(s.stdout)
		ch <- result{line, err}
	}()
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case r := <-ch:
		return r.line, r.err
	}
}

// shutdown closes stdin, stops the process, reaps it, and returns the trimmed
// stderr. Idempotent — safe to call from an error path and again via defer.
func (s *stdioSession) shutdown() string {
	s.once.Do(func() {
		_ = s.stdin.Close()
		s.cancel()
		<-s.waitDone
		_ = s.pr.Close()
		s.stderrStr = strings.TrimSpace(s.stderr.String())
	})
	return s.stderrStr
}

func (s *stdioSession) close() { s.shutdown() }

func (s *stdioSession) err(msg string, cause error) error {
	if detail := s.shutdown(); detail != "" {
		msg = msg + ": " + detail
	}
	return &MCPError{Server: s.server.Name, Transport: "stdio", Message: msg, Err: cause}
}

// callStdioStreaming is the ProtocolAuto stateless probe over stdio: same wire
// bytes as callStdio (params._meta, id 1), but the response is read from a live
// stream and the process is then torn down, instead of closing stdin and
// waiting for exit. This detects a handshake server's -32602 (or silence)
// promptly. Explicit ProtocolStateless keeps the unchanged callStdio.
func (c *Client) callStdioStreaming(server ServerConfig, request ToolRequest, timeout time.Duration) (Result, error) {
	sess, err := startStdioSession(server, timeout)
	if err != nil {
		return Result{}, err
	}
	defer sess.close()

	reqBytes, err := buildRequest(request)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "encoding request", Err: err}
	}
	if err := sess.write(reqBytes); err != nil {
		return Result{}, sess.err("writing request", err)
	}
	line, err := sess.readMessage()
	if err != nil {
		return Result{}, sess.err("process exited without a valid response", err)
	}
	return decodeResponse(server, line)
}

// callHandshakeStdio runs the full initialize / initialized / call sequence
// against a single long-lived stdio process.
func (c *Client) callHandshakeStdio(server ServerConfig, request ToolRequest, version string, timeout time.Duration) (Result, error) {
	sess, err := startStdioSession(server, timeout)
	if err != nil {
		return Result{}, err
	}
	defer sess.close()

	initReq, err := buildInitializeRequest(version, 1)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "encoding initialize", Err: err}
	}
	if err := sess.write(initReq); err != nil {
		return Result{}, sess.err("writing initialize", err)
	}
	initLine, err := sess.readMessage()
	if err != nil {
		return Result{}, sess.err("reading initialize response", err)
	}
	initResult, initErr := decodeInitializeResult(server, "stdio", initLine)
	if initErr != nil {
		return Result{}, initErr
	}

	notif, _ := buildInitializedNotification()
	if err := sess.write(notif); err != nil {
		return Result{}, sess.err("writing initialized notification", err)
	}

	if request.Method == "server/discover" {
		return initializeResultAsDiscoverResult(initResult)
	}

	realReq, err := buildBareRequest(request, 2)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "stdio", Message: "encoding request", Err: err}
	}
	if err := sess.write(realReq); err != nil {
		return Result{}, sess.err("writing request", err)
	}
	line, err := sess.readMessage()
	if err != nil {
		return Result{}, sess.err("reading response", err)
	}
	return decodeResponse(server, line)
}

// callHandshakeHTTP runs initialize / initialized / call as three POSTs over
// the Streamable HTTP transport, threading the negotiated protocol version and
// any assigned Mcp-Session-Id through the later requests.
func (c *Client) callHandshakeHTTP(server ServerConfig, request ToolRequest, version string, timeout time.Duration) (Result, error) {
	if server.URL == "" {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "no url configured"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	initReq, err := buildInitializeRequest(version, 1)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "encoding initialize", Err: err}
	}
	body, status, respHeaders, err := c.postHandshakeMessage(ctx, server, initReq, "initialize", version, "")
	if err != nil {
		return Result{}, err
	}
	if status < 200 || status >= 300 {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Code: status, Message: fmt.Sprintf("initialize: HTTP %d", status)}
	}
	initResult, initErr := decodeInitializeResult(server, "http", extractSSEData(body))
	if initErr != nil {
		return Result{}, initErr
	}
	sessionID := respHeaders.Get("Mcp-Session-Id")
	negotiated := initResult.ProtocolVersion
	if negotiated == "" {
		negotiated = version
	}

	notif, _ := buildInitializedNotification()
	_, status, _, err = c.postHandshakeMessage(ctx, server, notif, "notifications/initialized", negotiated, sessionID)
	if err != nil {
		return Result{}, err
	}
	if status != http.StatusAccepted && (status < 200 || status >= 300) {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Code: status, Message: fmt.Sprintf("initialized notification: HTTP %d", status)}
	}

	if request.Method == "server/discover" {
		return initializeResultAsDiscoverResult(initResult)
	}

	realReq, err := buildBareRequest(request, 2)
	if err != nil {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Message: "encoding request", Err: err}
	}
	body, status, _, err = c.postHandshakeMessage(ctx, server, realReq, request.Method, negotiated, sessionID)
	if err != nil {
		return Result{}, err
	}
	if status < 200 || status >= 300 {
		return Result{}, &MCPError{Server: server.Name, Transport: "http", Code: status, Message: fmt.Sprintf("HTTP %d", status)}
	}
	return decodeResponse(server, extractSSEData(body))
}

// postHandshakeMessage POSTs one JSON-RPC message. Unlike the stateless
// callHTTP it does not send Mcp-Method / Mcp-Name (2026-07-28-only mirrored
// headers a strict handshake server may reject), and it echoes MCP-Protocol-Version
// (spec MUST after initialize) and Mcp-Session-Id when one was assigned.
func (c *Client) postHandshakeMessage(ctx context.Context, server ServerConfig, payload []byte, method, protocolVersion, sessionID string) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, nil, &MCPError{Server: server.Name, Transport: "http", Message: method + ": building request", Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if protocolVersion != "" {
		req.Header.Set("MCP-Protocol-Version", protocolVersion)
	}
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	for k, v := range server.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, 0, nil, &MCPError{Server: server.Name, Transport: "http", Message: method + ": request failed", Err: err}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, resp.Header, &MCPError{Server: server.Name, Transport: "http", Message: method + ": reading response body", Err: err}
	}
	return b, resp.StatusCode, resp.Header, nil
}
