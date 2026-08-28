package a2a_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/a2a"
)

func decodeRPC(t *testing.T, r *http.Request) a2a.Request {
	t.Helper()
	body, _ := io.ReadAll(r.Body)
	var req a2a.Request
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("server: bad JSON-RPC request: %v", err)
	}
	return req
}

func writeResult(w http.ResponseWriter, result string) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":`+result+`}`)
}

func TestSendAndWaitReturnsMessageResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := decodeRPC(t, r).Method; got != "message/send" {
			t.Fatalf("method = %q, want message/send", got)
		}
		writeResult(w, `{"kind":"message","role":"agent","messageId":"m1","parts":[{"kind":"text","text":"bonjour"}]}`)
	}))
	defer srv.Close()

	cfg := a2a.Config{Name: "T", URL: srv.URL}
	raw, err := a2a.NewClient().SendAndWait(cfg, map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("SendAndWait: %v", err)
	}
	kind, _, _ := a2a.Inspect(raw)
	if kind != "message" {
		t.Fatalf("kind = %q, want message", kind)
	}
}

func TestSendAndWaitPollsTaskToTerminal(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(t, r)
		calls++
		switch req.Method {
		case "message/send":
			writeResult(w, `{"kind":"task","id":"t1","status":{"state":"working"}}`)
		case "tasks/get":
			if calls < 3 {
				writeResult(w, `{"kind":"task","id":"t1","status":{"state":"working"}}`)
				return
			}
			writeResult(w, `{"kind":"task","id":"t1","status":{"state":"completed"},"artifacts":[{"parts":[{"kind":"text","text":"done"}]}]}`)
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	defer srv.Close()

	cfg := a2a.Config{Name: "T", URL: srv.URL, PollInterval: 5 * time.Millisecond, PollTimeout: 2 * time.Second}
	raw, err := a2a.NewClient().SendAndWait(cfg, map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("SendAndWait: %v", err)
	}
	_, state, id := a2a.Inspect(raw)
	if state != "completed" || id != "t1" {
		t.Fatalf("state=%q id=%q, want completed/t1", state, id)
	}
}

func TestSendAndWaitTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeResult(w, `{"kind":"task","id":"t1","status":{"state":"working"}}`)
	}))
	defer srv.Close()

	cfg := a2a.Config{Name: "T", URL: srv.URL, PollInterval: 5 * time.Millisecond, PollTimeout: 30 * time.Millisecond}
	_, err := a2a.NewClient().SendAndWait(cfg, map[string]any{"message": "hi"})
	if err == nil || !strings.Contains(err.Error(), "terminal state") {
		t.Fatalf("expected a poll timeout error, got %v", err)
	}
}

func TestRPCErrorSurfacesTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"Task not found"}}`)
	}))
	defer srv.Close()

	cfg := a2a.Config{Name: "T", URL: srv.URL}
	_, err := a2a.NewClient().GetTask(cfg, "missing", 0)
	var aerr *a2a.A2AError
	if !errors.As(err, &aerr) {
		t.Fatalf("error = %v, want *a2a.A2AError", err)
	}
	if aerr.Code != -32001 || aerr.Method != "tasks/get" {
		t.Fatalf("A2AError = %+v", aerr)
	}
}

func TestCancelTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := decodeRPC(t, r).Method; got != "tasks/cancel" {
			t.Fatalf("method = %q, want tasks/cancel", got)
		}
		writeResult(w, `{"kind":"task","id":"t1","status":{"state":"canceled"}}`)
	}))
	defer srv.Close()

	cfg := a2a.Config{Name: "T", URL: srv.URL}
	raw, err := a2a.NewClient().CancelTask(cfg, "t1")
	if err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if _, state, _ := a2a.Inspect(raw); state != "canceled" {
		t.Fatalf("state = %q, want canceled", state)
	}
}

func TestAgentCardFetchesWellKnownPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-card.json" {
			t.Fatalf("path = %q, want /.well-known/agent-card.json", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Fatalf("missing auth header")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"name":"Translator","capabilities":{"streaming":false}}`)
	}))
	defer srv.Close()

	cfg := a2a.Config{Name: "T", URL: srv.URL + "/a2a", Headers: map[string]string{"Authorization": "Bearer tok"}}
	raw, err := a2a.NewClient().AgentCard(cfg)
	if err != nil {
		t.Fatalf("AgentCard: %v", err)
	}
	var card struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &card); err != nil || card.Name != "Translator" {
		t.Fatalf("card = %s err = %v", raw, err)
	}
}
