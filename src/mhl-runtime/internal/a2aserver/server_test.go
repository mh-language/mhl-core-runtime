package a2aserver_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/a2aserver"
)

func newTestServer(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h, err := a2aserver.Handler(dir, "http://test/", io.Discard)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func rpc(t *testing.T, url, method string, params any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST %s: %v", method, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s response: %v", method, err)
	}
	return out
}

const greet = `
pipeline Greet {
    input name: string
    var message = ""
    step Build { message = "hi " + name }
}
`

func TestAgentCard(t *testing.T) {
	ts := newTestServer(t, map[string]string{"g.mh": greet})

	for _, path := range []string{"/.well-known/agent-card.json", "/.well-known/agent.json"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var card map[string]any
		json.NewDecoder(resp.Body).Decode(&card)
		resp.Body.Close()

		if card["protocolVersion"] != "0.2" {
			t.Errorf("%s: protocolVersion = %v, want \"0.2\" (Major.Minor)", path, card["protocolVersion"])
		}
		if resp.Header.Get("A2A-Version") != "0.2" {
			t.Errorf("%s: A2A-Version header = %q", path, resp.Header.Get("A2A-Version"))
		}
		for _, req := range []string{"name", "description", "url", "version", "capabilities", "defaultInputModes", "defaultOutputModes", "skills"} {
			if _, ok := card[req]; !ok {
				t.Errorf("%s: Agent Card missing required field %q", path, req)
			}
		}
		skills := card["skills"].([]any)
		if len(skills) != 1 || skills[0].(map[string]any)["id"] != "Greet" {
			t.Fatalf("%s: skills = %v, want one skill id=Greet", path, skills)
		}
	}
}

func TestMessageSendTaskLifecycle(t *testing.T) {
	ts := newTestServer(t, map[string]string{"g.mh": greet})

	send := rpc(t, ts.URL+"/", "message/send", map[string]any{
		"message": map[string]any{
			"role":  "user",
			"parts": []map[string]any{{"kind": "text", "text": "go"}},
			"metadata": map[string]any{
				"skill": "Greet",
				"input": map[string]any{"name": "ana"},
			},
		},
	})
	task := send["result"].(map[string]any)
	id := task["id"].(string)
	if id == "" || task["kind"] != "task" {
		t.Fatalf("message/send returned %v", task)
	}

	// Poll tasks/get until terminal.
	var got map[string]any
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got = rpc(t, ts.URL+"/", "tasks/get", map[string]any{"id": id})["result"].(map[string]any)
		state := got["status"].(map[string]any)["state"].(string)
		if state == "completed" || state == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	status := got["status"].(map[string]any)
	if status["state"] != "completed" {
		t.Fatalf("final state = %v (%v)", status["state"], status)
	}
	arts := got["artifacts"].([]any)
	text := arts[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hi ana") {
		t.Errorf("artifact text = %q, want it to contain %q", text, "hi ana")
	}
}

func TestMessageSendUnknownSkill(t *testing.T) {
	ts := newTestServer(t, map[string]string{
		"a.mh": greet,
		"b.mh": "pipeline Other {\n    step S { var x = 1 }\n}\n",
	})

	// No skill + multiple workflows → error.
	r := rpc(t, ts.URL+"/", "message/send", map[string]any{"message": map[string]any{}})
	if r["error"] == nil {
		t.Fatalf("expected an error when no skill is named and there are 2 skills: %v", r)
	}

	// Named skill that doesn't exist → error.
	r = rpc(t, ts.URL+"/", "message/send", map[string]any{
		"message": map[string]any{"metadata": map[string]any{"skill": "Nope"}},
	})
	if r["error"] == nil {
		t.Fatalf("expected an unknown-skill error: %v", r)
	}
}

// message/send enforces the skill's advertised inputSchema before a task is
// created: a missing required input, or an undeclared one, is a -32602 error.
func TestMessageSendEnforcesInputSchema(t *testing.T) {
	ts := newTestServer(t, map[string]string{"g.mh": greet})

	// Missing the required `name` input.
	r := rpc(t, ts.URL+"/", "message/send", map[string]any{
		"message": map[string]any{"metadata": map[string]any{"skill": "Greet"}},
	})
	e, ok := r["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected a -32602 error for the missing input, got %v", r)
	}
	if e["code"].(float64) != -32602 {
		t.Errorf("code = %v, want -32602", e["code"])
	}

	// Undeclared input.
	r = rpc(t, ts.URL+"/", "message/send", map[string]any{
		"message": map[string]any{"metadata": map[string]any{
			"skill": "Greet", "input": map[string]any{"name": "ana", "extra": 1},
		}},
	})
	if r["error"] == nil {
		t.Fatalf("expected a -32602 error for the undeclared input, got %v", r)
	}
}

// configuration.blocking:true holds the response until the task is terminal.
func TestMessageSendBlocking(t *testing.T) {
	ts := newTestServer(t, map[string]string{"g.mh": greet})

	r := rpc(t, ts.URL+"/", "message/send", map[string]any{
		"message":       map[string]any{"metadata": map[string]any{"skill": "Greet", "input": map[string]any{"name": "bo"}}},
		"configuration": map[string]any{"blocking": true},
	})
	task := r["result"].(map[string]any)
	if s := task["status"].(map[string]any)["state"]; s != "completed" {
		t.Fatalf("blocking message/send returned state %v, want \"completed\"", s)
	}
	text := task["artifacts"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hi bo") {
		t.Errorf("artifact text = %q", text)
	}
}

// tasks/cancel on an already-terminal task is -32002 (TaskNotCancelableError).
func TestTasksCancelTerminal(t *testing.T) {
	ts := newTestServer(t, map[string]string{"g.mh": greet})

	send := rpc(t, ts.URL+"/", "message/send", map[string]any{
		"message":       map[string]any{"metadata": map[string]any{"skill": "Greet", "input": map[string]any{"name": "x"}}},
		"configuration": map[string]any{"blocking": true},
	})
	id := send["result"].(map[string]any)["id"].(string)

	c := rpc(t, ts.URL+"/", "tasks/cancel", map[string]any{"id": id})
	e, ok := c["error"].(map[string]any)
	if !ok {
		t.Fatalf("cancel of a completed task should error, got %v", c)
	}
	if e["code"].(float64) != -32002 {
		t.Errorf("cancel-terminal code = %v, want -32002", e["code"])
	}
}
