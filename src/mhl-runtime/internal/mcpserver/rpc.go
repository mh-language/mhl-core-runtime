package mcpserver

import "encoding/json"

// rpcMsg is the JSON-RPC 2.0 envelope this server reads and writes. id is a
// json.RawMessage so a string, number, or null id round-trips unchanged;
// Result is likewise raw so a legitimate `"result": null` (e.g. an empty
// object reply serialized to nothing) stays present rather than being
// stripped by omitempty.
type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
