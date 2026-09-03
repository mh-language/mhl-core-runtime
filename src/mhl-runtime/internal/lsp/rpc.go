// Package lsp implements a Language Server Protocol server for .mh files,
// exposed as `mhl lsp` (internal/cli). It runs over stdio using the LSP
// wire format (HTTP-style "Content-Length" framed JSON-RPC 2.0) and reuses
// internal/lang/parser and internal/lang/lint for the actual language
// knowledge — this package only speaks the protocol and turns editor events
// (open/change/close, completion requests) into calls against them.
//
// Scope is deliberately an MVP: diagnostics (parse errors + lint findings,
// pushed on open/change), completion (keywords, declared symbol names, and
// member-call suggestions after "name."), signature help, and
// go-to-definition for declared names (and `from "..."` import paths). No
// hover or incremental sync yet — full-document sync only.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// rpcMessage is the generic envelope for every JSON-RPC message this server
// reads or writes. id is omitted (nil) on notifications. Result is a
// json.RawMessage, not `any`, specifically so a legitimate null result (the
// spec-mandated response to e.g. "shutdown") still serializes as a present
// "result":null key: an `any` field holding a boxed untyped nil is itself a
// nil *interface*, which `omitempty` strips entirely — producing a response
// with neither "result" nor "error", which a strict client (vscode's
// jsonrpc) rejects with "the received response has neither a result nor an
// error property". A json.RawMessage set to the 4-byte literal "null" has
// len 4, so omitempty (which keys off len==0 for slice-kind fields) leaves
// it in place.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// reader reads Content-Length framed JSON-RPC messages off r.
type reader struct {
	br *bufio.Reader
}

func newReader(r io.Reader) *reader {
	return &reader{br: bufio.NewReader(r)}
}

// next blocks for the next framed message, returning io.EOF when the stream
// closes (the client disconnected without a clean "exit" notification).
func (rd *reader) next() (*rpcMessage, error) {
	length := -1
	for {
		line, err := rd.br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("lsp: malformed Content-Length header %q: %w", value, err)
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("lsp: message with no Content-Length header")
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(rd.br, body); err != nil {
		return nil, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("lsp: decoding message: %w", err)
	}
	return &msg, nil
}

// writer writes Content-Length framed JSON-RPC messages to w. Every write
// is one atomic Write call so concurrent notifications (diagnostics) and
// responses never interleave their frames.
type writer struct {
	w io.Writer
}

func newWriter(w io.Writer) *writer {
	return &writer{w: w}
}

func (wr *writer) send(msg rpcMessage) error {
	msg.JSONRPC = "2.0"
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	_, err = io.WriteString(wr.w, frame)
	return err
}

func (wr *writer) respond(id json.RawMessage, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return wr.send(rpcMessage{ID: id, Result: raw})
}

func (wr *writer) respondError(id json.RawMessage, code int, message string) error {
	return wr.send(rpcMessage{ID: id, Error: &rpcError{Code: code, Message: message}})
}

func (wr *writer) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return wr.send(rpcMessage{Method: method, Params: raw})
}
