package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestA2AAgentSendPollsTaskToCompletion proves the end-to-end wiring for
// `<a2a_agent>.send(...)`: a JSON-RPC `message/send` that returns a
// non-terminal Task, `tasks/get` polling until the task completes, and the
// normalized result (`.text`, `.state`) landing back as an ordinary MHL
// object. The fake server stands in for a real A2A agent.
func TestA2AAgentSendPollsTaskToCompletion(t *testing.T) {
	var gotAuth string
	var methods []string
	var gets int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		methods = append(methods, req.Method)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "message/send":
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"kind":"task","id":"task-1","status":{"state":"working"}}}`)
		case "tasks/get":
			gets++
			if gets < 2 {
				io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"kind":"task","id":"task-1","status":{"state":"working"}}}`)
				return
			}
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"kind":"task","id":"task-1","status":{"state":"completed"},"artifacts":[{"parts":[{"kind":"text","text":"bonjour le monde"}]}]}}`)
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	defer server.Close()

	t.Setenv("A2A_TOKEN", "sk-test-1")

	src := `
a2a_agent Translator {
    url: "` + server.URL + `/a2a"
    headers: {
        "Authorization": "Bearer " + env("A2A_TOKEN")
    }
    poll_interval: 5ms
    poll_timeout: 2s
}
` + wrapStep(`
        var r = Translator.send("Traduza 'hello world' para francês")
        log("kind: ${r.kind}")
        log("state: ${r.state}")
        log("text: ${r.text}")
        log("task_id: ${r.task_id}")
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotAuth != "Bearer sk-test-1" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-test-1")
	}
	if len(methods) < 3 || methods[0] != "message/send" {
		t.Errorf("methods = %v, want message/send then tasks/get polling", methods)
	}
	for _, want := range []string{"kind: task\n", "state: completed\n", "text: bonjour le monde\n", "task_id: task-1\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
}

// TestA2AAgentSendRejectsInputRequired proves a task that stops in
// `input-required` is surfaced as a clear error, not returned as if it were
// the answer.
func TestA2AAgentSendRejectsInputRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"kind":"task","id":"t","status":{"state":"input-required"}}}`)
	}))
	defer server.Close()

	src := `
a2a_agent Helper {
    url: "` + server.URL + `"
}
` + wrapStep(`
        var r = Helper.send("do the thing")
        log(r.text)
    `)

	_, err := run(t, src)
	if err == nil || !strings.Contains(err.Error(), "waiting on additional input") {
		t.Fatalf("expected an input-required rejection, got %v", err)
	}
}

// TestA2AAgentCardFetchesWellKnown proves `.agent_card()` GETs the well-known
// path and decodes the card as an MHL object.
func TestA2AAgentCardFetchesWellKnown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/.well-known/agent-card.json" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"name":"Translator","description":"translates text"}`)
	}))
	defer server.Close()

	src := `
a2a_agent Translator {
    url: "` + server.URL + `/a2a"
}
` + wrapStep(`
        var card = Translator.agent_card()
        log("name: ${card.name}")
        log("description: ${card.description}")
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "name: Translator\n") || !strings.Contains(out, "description: translates text\n") {
		t.Errorf("output missing decoded card fields, got:\n%s", out)
	}
}
