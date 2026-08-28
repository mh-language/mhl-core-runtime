package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestShouldFallBack(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"invalid params", &MCPError{Code: -32602}, true},
		{"invalid request", &MCPError{Code: -32600}, true},
		{"http 400", &MCPError{Code: http.StatusBadRequest}, true},
		{"method not found", &MCPError{Code: -32601}, false},
		{"parse error", &MCPError{Code: -32700}, false},
		{"http 500", &MCPError{Code: 500}, false},
		{"transport failure / no response", &MCPError{Code: 0}, false},
		{"not an MCPError", errors.New("boom"), false},
		{"wrapped MCPError", fmt.Errorf("x: %w", &MCPError{Code: -32602}), true},
	}
	for _, tc := range cases {
		if got := shouldFallBack(tc.err); got != tc.want {
			t.Errorf("%s: shouldFallBack = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBuildBareRequestOmitsMeta(t *testing.T) {
	b, err := buildBareRequest(ToolRequest{
		Method: "tools/call",
		Params: map[string]interface{}{"name": "x", "arguments": map[string]any{"q": 1}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "_meta") {
		t.Errorf("bare request must not contain _meta: %s", b)
	}
	var req Request
	if err := json.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	if req.ID != 2 || req.Method != "tools/call" {
		t.Errorf("unexpected request: %+v", req)
	}
}

func TestBuildInitializeRequestAdvertisesVersion(t *testing.T) {
	b, err := buildInitializeRequest(HandshakeVersionPrev, 1)
	if err != nil {
		t.Fatal(err)
	}
	var req Request
	if err := json.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "initialize" {
		t.Errorf("method = %q", req.Method)
	}
	var p handshakeInitializeParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.ProtocolVersion != HandshakeVersionPrev {
		t.Errorf("protocolVersion = %q, want %q", p.ProtocolVersion, HandshakeVersionPrev)
	}
	if p.ClientInfo == nil || p.ClientInfo.Name != "mhl" {
		t.Errorf("clientInfo = %+v, want {name: mhl}", p.ClientInfo)
	}
	if p.ClientInfo != nil && p.ClientInfo.Version == "" {
		t.Error("clientInfo.version must be non-empty — some servers reject an empty version")
	}
	if p.Capabilities == nil {
		t.Error("capabilities must be present (empty object)")
	}
}
