// Package mcp implements a Model Context Protocol (MCP) client. It speaks
// JSON-RPC 2.0 over two transports — stdio (a locally spawned process) and
// HTTP/SSE (a remote endpoint with header-based auth) — under a single client
// contract:
//
//	Client.CallTool(server, request) (Result, error)
//
// By default (Protocol "" / ProtocolAuto) a call is first attempted in the
// stateless SpecVersion form: no initialize/initialized handshake, protocol
// context carried in params._meta, each invocation fully independent. A server
// that rejects that form with a protocol-incompatibility error (JSON-RPC
// -32602/-32600, or HTTP 400) triggers an automatic fallback to the standard
// initialize/notifications/initialized handshake used by MCP revisions
// 2025-11-25 and 2025-06-18. See ParseProtocol for pinning either mode.
package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSONRPCVersion is the JSON-RPC protocol version used by all requests.
const JSONRPCVersion = "2.0"

// SpecVersion is the stateless MCP revision this client speaks on its first
// (and, for a stateless server, only) attempt.
const SpecVersion = "2026-07-28"

// Handshake revisions this client can fall back to. Both use the identical
// initialize/notifications/initialized lifecycle; the constant only selects
// which protocolVersion string the initialize request advertises. A server may
// still negotiate the effective version down (e.g. to 2025-03-26) in its
// initialize response.
const (
	HandshakeVersionLatest = "2025-11-25"
	HandshakeVersionPrev   = "2025-06-18"
)

// Protocol selects how CallTool talks to a server.
type Protocol string

const (
	// ProtocolAuto tries SpecVersion (stateless) first and falls back to the
	// handshake advertising HandshakeVersionLatest on a protocol-incompatibility
	// error. It is the zero value's meaning.
	ProtocolAuto Protocol = "auto"
	// ProtocolStateless pins the stateless SpecVersion form with no fallback.
	ProtocolStateless Protocol = "2026-07-28"
	// ProtocolHandshake2511 pins the handshake, advertising 2025-11-25.
	ProtocolHandshake2511 Protocol = "2025-11-25"
	// ProtocolHandshake2506 pins the handshake, advertising 2025-06-18.
	ProtocolHandshake2506 Protocol = "2025-06-18"
)

// ParseProtocol normalizes a declared `protocol:` value. An empty string means
// ProtocolAuto. Any value other than the four recognized ones is an error.
func ParseProtocol(raw string) (Protocol, error) {
	switch strings.TrimSpace(raw) {
	case "", string(ProtocolAuto):
		return ProtocolAuto, nil
	case string(ProtocolStateless):
		return ProtocolStateless, nil
	case string(ProtocolHandshake2511):
		return ProtocolHandshake2511, nil
	case string(ProtocolHandshake2506):
		return ProtocolHandshake2506, nil
	default:
		return "", fmt.Errorf("mcp_server protocol %q is not supported — use %q, %q, %q, or %q",
			raw, ProtocolAuto, ProtocolStateless, ProtocolHandshake2511, ProtocolHandshake2506)
	}
}

// advertisedVersion is the protocolVersion string a handshake in this mode puts
// in its initialize request. Only meaningful for the handshake protocols;
// ProtocolAuto's fallback advertises HandshakeVersionLatest.
func (p Protocol) advertisedVersion() string {
	switch p {
	case ProtocolHandshake2506:
		return HandshakeVersionPrev
	default:
		return HandshakeVersionLatest
	}
}

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

// handshakeInitializeParams is the `params` of the `initialize` request that
// opens a handshake session (MCP 2025-11-25 / 2025-06-18 §Lifecycle). mhl is a
// minimal client: it advertises no optional capabilities and rejects
// elicitation/sampling round-trips, so Capabilities is always an empty object.
type handshakeInitializeParams struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ClientInfo      *ClientInfo            `json:"clientInfo"`
}

// HandshakeInitializeResult is the `result` of the `initialize` response. The
// server MAY answer with a ProtocolVersion older than the one advertised; the
// client honours that negotiated value on the HTTP MCP-Protocol-Version header
// and returns this whole payload from `.discover()` in handshake mode (the
// `server/discover` method does not exist before 2026-07-28).
type HandshakeInitializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities,omitempty"`
	ServerInfo      map[string]interface{} `json:"serverInfo,omitempty"`
	Instructions    string                 `json:"instructions,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification envelope — a request with no `id`,
// to which the server sends no response (HTTP: 202 Accepted, no body). Used for
// `notifications/initialized`.
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

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
