package lsp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestRespondNilResultIncludesResultKey guards against the bug that broke
// "Restart Language Server" in the VS Code client: shutdown's spec-mandated
// null result (server.go's `s.wr.respond(msg.ID, nil)`) must still produce a
// response with a "result" key, or vscode-languageclient rejects it with
// "the received response has neither a result nor an error property".
func TestRespondNilResultIncludesResultKey(t *testing.T) {
	var buf bytes.Buffer
	wr := newWriter(&buf)
	if err := wr.respond(json.RawMessage(`1`), nil); err != nil {
		t.Fatalf("respond: %v", err)
	}

	frame := buf.String()
	_, body, ok := strings.Cut(frame, "\r\n\r\n")
	if !ok {
		t.Fatalf("malformed frame: %q", frame)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	if _, hasResult := decoded["result"]; !hasResult {
		t.Fatalf("response has neither a result nor an error property: %s", body)
	}
	if _, hasError := decoded["error"]; hasError {
		t.Errorf("expected no error key alongside a nil-result success response, got: %s", body)
	}
}
