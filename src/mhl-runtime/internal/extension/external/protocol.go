// Package external runs an extension as a persistent child process and speaks
// to it over newline-delimited JSON-RPC on stdin/stdout — the Bloc B host
// from docs/plano-extensoes-mcp-a2a.md. One process is started per extension
// on first use, kept alive for the rest of the run, multiplexed by request
// id, and shut down gracefully at the end. It never starts a process per
// method call.
//
// The wire payloads are the same DTOs internal/extension already defines
// (CallRequest, Value, Diagnostic, ...), so this transport is a projection of
// the in-process contract, not a second model.
package external

import (
	"encoding/json"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// APIVersion is the wire-protocol revision this host speaks. An extension
// whose manifest / handshake advertises a different major is rejected.
const APIVersion = "1"

// message is one line on the wire, in either direction. A response carries
// ID + (Result xor Error) and no Method. A request carries ID + Method +
// Params. A notification carries Method + Params and no ID.
type message struct {
	ID     *uint64         `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *wireError      `json:"error,omitempty"`
}

func (m message) isResponse() bool     { return m.ID != nil && m.Method == "" }
func (m message) isNotification() bool { return m.ID == nil && m.Method != "" }
func (m message) isRequest() bool      { return m.ID != nil && m.Method != "" }

// wireError is a structured failure an extension reports for a call.
type wireError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

func (e *wireError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Code != "" {
		return e.Code + ": " + e.Message
	}
	return e.Message
}

// --- host -> extension --------------------------------------------------

// initializeParams is the first message the host sends after spawning.
type initializeParams struct {
	APIVersion  string `json:"api_version"`
	Host        string `json:"host"`
	HostVersion string `json:"host_version"`
}

// initializeResult is the extension's handshake reply. Declarations is
// optional: when present, it is the extension's own description of the kinds
// and methods it serves — `mhl extension package` captures it into the
// manifest's sidecar so an author declares the surface once, in code. At run
// time the manifest remains the source lint and the LSP read.
type initializeResult struct {
	APIVersion string `json:"api_version"`
	Extension  struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	} `json:"extension"`
	Declarations []extension.DeclarationSpec `json:"declarations,omitempty"`
}

// callParams carries one method invocation. It mirrors extension.CallRequest
// with the method pulled out to Operation.
type callParams struct {
	Declaration json.RawMessage            `json:"declaration"`
	Operation   string                     `json:"operation"`
	Args        []json.RawMessage          `json:"args,omitempty"`
	NamedArgs   map[string]json.RawMessage `json:"named_args,omitempty"`
}

// --- extension -> host -------------------------------------------------

// logParams is an extension-initiated log line (notification).
type logParams struct {
	Message string `json:"message"`
}

// secretResolveParams is an extension-initiated request for a credential.
// The host checks it, resolves it, and replies with the value or an error —
// the extension never receives the ambient environment.
type secretResolveParams struct {
	Reference string `json:"reference"`
}
