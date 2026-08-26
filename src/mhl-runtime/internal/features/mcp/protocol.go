// Package mcp implements a stateless Model Context Protocol (MCP) client
// conforming to spec 2026-07-28. It speaks JSON-RPC 2.0 over two transports —
// stdio (a locally spawned process) and HTTP/SSE (a remote endpoint with
// header-based auth) — under a single client contract:
//
//	Client.CallTool(server, request) (Result, error)
//
// No client-side session or handshake state is retained between calls: each
// invocation is fully independent (RF-3, RF-4, ADR-3).
package mcp

import (
	"encoding/json"
	"fmt"
)

// JSONRPCVersion is the JSON-RPC protocol version used by all requests.
const JSONRPCVersion = "2.0"

// SpecVersion is the MCP specification revision this client conforms to.
const SpecVersion = "2026-07-28"

// Meta models the JSON-RPC `_meta` object of a *result*. Per spec 2026-07-28
// it may carry a `ttlMs` field indicating how long a tool result may be
// considered fresh.
type Meta struct {
	TTLMs int64 `json:"ttlMs,omitempty"`
}

// RequestMeta models the `_meta` object spec 2026-07-28 requires inside
// every request's `params` — this is what replaced the `initialize`
// handshake and protocol-level sessions removed in this revision (see
// "Statelessness" and "General fields > _meta > Per-request protocol
// fields" in the spec): a server infers nothing about protocol version or
// client capabilities from prior requests on the same connection, so every
// request must restate them itself. A request missing ProtocolVersion or
// ClientCapabilities is malformed and MUST be rejected by a conformant
// server with JSON-RPC error -32602 (Invalid params) / HTTP 400.
// ClientInfo is optional (spec: "SHOULD include... unless specifically
// configured not to") and is always populated by this client with a fixed
// self-identification — see ClientInfo's own doc comment for why it carries
// no version.
type RequestMeta struct {
	ProtocolVersion    string                 `json:"io.modelcontextprotocol/protocolVersion"`
	ClientCapabilities map[string]interface{} `json:"io.modelcontextprotocol/clientCapabilities"`
	ClientInfo         *ClientInfo            `json:"io.modelcontextprotocol/clientInfo,omitempty"`
}

// ClientInfo self-identifies this client in a request's `_meta` — purely
// informational (spec: "not verified by the protocol... intended for
// display, logging, and debugging"). Version is deliberately left empty:
// this package sits below internal/cli in mhl's dependency order (lang →
// engine → features → cli), so it has no access to cli.Version, and adding
// a second, package-local version constant that could drift from the real
// one would be worse than reporting none.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// mhlClientInfo is the fixed self-identification every request carries.
var mhlClientInfo = &ClientInfo{Name: "mhl"}

// Request is a JSON-RPC 2.0 request envelope.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Meta    *Meta           `json:"_meta,omitempty"`
}

// Response is a JSON-RPC 2.0 response envelope.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	Meta    *Meta           `json:"_meta,omitempty"`
}

// RPCError is the JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// MCPError is the typed error surfaced by the client for any transport-level or
// protocol-level failure. It carries enough context to identify the server and
// transport without leaking residual session state.
type MCPError struct {
	Server    string // declared mcp_server name
	Transport string // "stdio" or "http"
	Code      int    // JSON-RPC error code, or 0 for transport failures
	Message   string
	Err       error // underlying cause, if any
}

func (e *MCPError) Error() string {
	base := fmt.Sprintf("mcp: %s transport call to %q failed: %s", e.Transport, e.Server, e.Message)
	if e.Code != 0 {
		base = fmt.Sprintf("%s (code %d)", base, e.Code)
	}
	if e.Err != nil {
		return base + ": " + e.Err.Error()
	}
	return base
}

// Unwrap exposes the underlying cause for errors.Is/As.
func (e *MCPError) Unwrap() error { return e.Err }
