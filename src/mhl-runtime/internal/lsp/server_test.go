package lsp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// newTestServer returns a server whose writer collects framed responses into
// buf, for driving s.handle directly.
func newTestServer(buf *bytes.Buffer) *server {
	return &server{wr: newWriter(buf), docs: map[string]string{}}
}

// lastResult parses the final JSON-RPC frame written to buf and returns its
// raw "result" value.
func lastResult(t *testing.T, buf *bytes.Buffer) json.RawMessage {
	t.Helper()
	frames := strings.Split(buf.String(), "Content-Length:")
	last := strings.TrimSpace(frames[len(frames)-1])
	_, body, ok := strings.Cut(last, "\r\n\r\n")
	if !ok {
		t.Fatalf("malformed frame: %q", last)
	}
	var msg struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatalf("decoding response: %v (%s)", err, body)
	}
	return msg.Result
}

func TestInitializeAdvertisesDefinitionProvider(t *testing.T) {
	var buf bytes.Buffer
	s := newTestServer(&buf)
	s.handle(&rpcMessage{ID: json.RawMessage(`1`), Method: "initialize", Params: json.RawMessage(`{}`)})

	var res struct {
		Capabilities struct {
			DefinitionProvider bool `json:"definitionProvider"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(lastResult(t, &buf), &res); err != nil {
		t.Fatalf("decoding initialize result: %v", err)
	}
	if !res.Capabilities.DefinitionProvider {
		t.Error("initialize did not advertise definitionProvider")
	}
}

func TestHandleDefinitionReturnsLocation(t *testing.T) {
	var buf bytes.Buffer
	s := newTestServer(&buf)
	const uri = "file:///proj/main.mh"
	s.docs[uri] = "agent Reviewer {}\n\npipeline P {\n  step S { var x = Reviewer.run(\"hi\") }\n}\n"

	params, _ := json.Marshal(textDocumentPositionParams{
		TextDocument: versionedTextDocumentIdentifier{URI: uri},
		Position:     position{Line: 3, Character: 20}, // on "Reviewer"
	})
	s.handle(&rpcMessage{ID: json.RawMessage(`2`), Method: "textDocument/definition", Params: params})

	var locs []location
	if err := json.Unmarshal(lastResult(t, &buf), &locs); err != nil {
		t.Fatalf("decoding definition result: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("want 1 location, got %+v", locs)
	}
	if locs[0].URI != uri || locs[0].Range.Start.Line != 0 {
		t.Errorf("location = %+v, want %s @ line 0", locs[0], uri)
	}
}

func TestHandleDefinitionNoMatchIsNull(t *testing.T) {
	var buf bytes.Buffer
	s := newTestServer(&buf)
	const uri = "file:///proj/main.mh"
	s.docs[uri] = "pipeline P {\n  step S { var x = Missing.run() }\n}\n"

	params, _ := json.Marshal(textDocumentPositionParams{
		TextDocument: versionedTextDocumentIdentifier{URI: uri},
		Position:     position{Line: 1, Character: 20},
	})
	s.handle(&rpcMessage{ID: json.RawMessage(`3`), Method: "textDocument/definition", Params: params})

	if got := strings.TrimSpace(string(lastResult(t, &buf))); got != "null" {
		t.Errorf("result = %s, want null", got)
	}
}
