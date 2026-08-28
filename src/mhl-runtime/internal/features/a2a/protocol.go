// Package a2a implements a stateless client for the Agent2Agent (A2A)
// protocol, revision 0.2.x: JSON-RPC 2.0 over HTTP(S) with header-based
// authentication. It speaks four operations under a single client contract:
//
//	Client.SendMessage(cfg, params) (json.RawMessage, error)
//	Client.GetTask(cfg, id, historyLength) (json.RawMessage, error)
//	Client.CancelTask(cfg, id) (json.RawMessage, error)
//	Client.AgentCard(cfg) (json.RawMessage, error)
//
// No client-side session or handshake state is retained between calls: each
// invocation is fully independent, mirroring internal/features/mcp. A
// blocking `.send` that polls `tasks/get` to a terminal state issues several
// independent requests but keeps no state beyond the task id it was handed.
package a2a

import (
	"encoding/json"
	"fmt"
)

// JSONRPCVersion is the JSON-RPC protocol version used by all requests.
const JSONRPCVersion = "2.0"

// ProtocolVersion is the A2A specification revision this client targets. The
// 0.2.x line is what deployed A2A agents and SDKs implement today: method
// names `message/send`, `tasks/get`, `tasks/cancel`, and an Agent Card at
// `/.well-known/agent-card.json`. The 1.0 draft renames these
// (`a2a.SendMessage`, `/.well-known/a2a/agent-card`) and is a later change.
const ProtocolVersion = "0.2.6"

// Request is a JSON-RPC 2.0 request envelope.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response envelope.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// A2AError is the typed error surfaced by the client for any transport-level
// or protocol-level failure. It carries enough context to identify the
// declared agent without leaking residual session state.
type A2AError struct {
	Agent   string // declared a2a_agent name
	Method  string // A2A method or "agent_card"
	Code    int    // JSON-RPC error code, or HTTP status, or 0 for transport failures
	Message string
	Err     error // underlying cause, if any
}

func (e *A2AError) Error() string {
	base := fmt.Sprintf("a2a: call %q to agent %q failed: %s", e.Method, e.Agent, e.Message)
	if e.Code != 0 {
		base = fmt.Sprintf("%s (code %d)", base, e.Code)
	}
	if e.Err != nil {
		return base + ": " + e.Err.Error()
	}
	return base
}

// Unwrap exposes the underlying cause for errors.Is/As.
func (e *A2AError) Unwrap() error { return e.Err }
