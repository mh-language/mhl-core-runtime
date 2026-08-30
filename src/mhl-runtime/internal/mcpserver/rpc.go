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

// resultMsg builds a success response for id carrying result. A marshal
// failure degrades to an internal-error response so the caller always has
// something to send.
func resultMsg(id json.RawMessage, result any) *rpcMsg {
	raw, err := json.Marshal(result)
	if err != nil {
		return errMsg(id, -32603, "marshal result: "+err.Error())
	}
	return &rpcMsg{ID: id, Result: raw}
}

// errMsg builds an error response for id.
func errMsg(id json.RawMessage, code int, msg string) *rpcMsg {
	return &rpcMsg{ID: id, Error: &rpcErr{Code: code, Message: msg}}
}

// errData is errMsg with a JSON-RPC error `data` payload.
func errData(id json.RawMessage, code int, msg string, data any) *rpcMsg {
	return &rpcMsg{ID: id, Error: &rpcErr{Code: code, Message: msg, Data: data}}
}
