// Package a2aserver exposes a directory of mhl pipelines/workflows as an
// Agent2Agent (A2A) agent: one skill per declaration, `message/send` starts
// a run as an A2A task, and `tasks/get` / `tasks/cancel` drive its
// lifecycle. Revision 0.2.x — JSON-RPC 2.0 over HTTP, an Agent Card at
// /.well-known/agent-card.json.
//
// Skill selection is explicit (routing option "b"): the caller names the
// skill and passes structured inputs in the message metadata —
//
//	message.metadata.skill  = "<WorkflowName>"   (or params.metadata.skill)
//	message.metadata.input  = { ... }            (validated against the
//	                                              workflow's declared inputs)
//
// A message with no skill runs the only workflow when there is exactly one,
// and is an error otherwise. Text parts are not auto-mapped to inputs.
//
// This is the server counterpart to internal/features/a2a (the client); they
// share no code.
package a2aserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// ServerVersion is reported in the Agent Card. internal/cli overwrites it
// with the build version at startup.
var ServerVersion = "0"

// protocolVersion is the A2A revision this server implements — Major.Minor
// only, per the spec ("Patch version numbers SHOULD NOT be used in requests,
// responses and Agent Cards"). The 0.2.x line is what deployed A2A agents
// and SDKs speak today; 1.0.0 is a later rename.
const protocolVersion = "0.2"

// taskTTL bounds how long a finished task stays queryable; a message/send
// sweeps anything older.
const taskTTL = time.Hour

// blockingWait caps how long a `configuration.blocking: true` message/send
// holds the HTTP request waiting for the task to reach a terminal state
// before returning the still-working Task.
const blockingWait = 30 * time.Second

// Serve loads every .mh file under dir and serves the A2A agent on addr
// until ctx is done. logw receives startup and per-request diagnostics. The
// caller registers session extensions (interpreter.SetSessionExtensions)
// beforehand.
func Serve(ctx context.Context, addr, dir string, logw io.Writer) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	baseURL := "http://" + ln.Addr().String() + "/"
	handler, s, err := build(dir, baseURL, logw)
	if err != nil {
		return err
	}
	fmt.Fprintf(logw, "mhl serve a2a: %d skill(s) from %s on %s\n", len(s.workflows), dir, baseURL)

	httpSrv := &http.Server{Handler: handler}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(sh)
	}()
	err = httpSrv.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

type server struct {
	workflows map[string]execsvc.Workflow
	baseURL   string
	logw      io.Writer

	mu    sync.Mutex
	tasks map[string]*task
}

// build loads dir and returns the HTTP handler plus the server (exposed for
// tests via Handler). baseURL is what the Agent Card advertises as its
// endpoint.
func build(dir, baseURL string, logw io.Writer) (http.Handler, *server, error) {
	wf, err := execsvc.Load(dir)
	if err != nil {
		return nil, nil, err
	}
	s := &server{workflows: wf, baseURL: baseURL, logw: logw, tasks: map[string]*task{}}
	mux := http.NewServeMux()
	// Current convention is /.well-known/agent-card.json; earlier 0.2.x
	// clients look for /.well-known/agent.json — serve both.
	mux.HandleFunc("/.well-known/agent-card.json", s.handleCard)
	mux.HandleFunc("/.well-known/agent.json", s.handleCard)
	mux.HandleFunc("/", s.handleRPC)
	return mux, s, nil
}

// Handler builds the A2A HTTP handler for the workflows under dir — for
// embedding in another server or for tests. Serve is this plus an
// http.Server and signal handling.
func Handler(dir, baseURL string, logw io.Writer) (http.Handler, error) {
	h, _, err := build(dir, baseURL, logw)
	return h, err
}

type task struct {
	id       string
	skill    string
	state    string
	created  time.Time
	updated  time.Time
	result   *execsvc.Result
	errMsg   string
	cancel   context.CancelFunc
	done     chan struct{} // closed once the task reaches a terminal state
	doneOnce sync.Once
}

func (t *task) markDone() { t.doneOnce.Do(func() { close(t.done) }) }

func (s *server) handleCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.agentCard())
}

func (s *server) agentCard() map[string]any {
	names := s.sortedNames()
	skills := make([]map[string]any, 0, len(names))
	for _, n := range names {
		wf := s.workflows[n]
		desc := wf.Pipeline.Description
		if desc == "" {
			desc = fmt.Sprintf("Runs the mhl %s %q.", wf.KindLabel(), n)
		}
		desc += " Pass inputs as message.metadata.input."
		skills = append(skills, map[string]any{
			"id":          n,
			"name":        n,
			"description": desc,
			"tags":        []string{"mhl"},
			"inputModes":  []string{"application/json"},
			"outputModes": []string{"application/json"},
			// Non-standard hint: the JSON Schema of this skill's inputs.
			"metadata": map[string]any{"inputSchema": wf.Pipeline.InputSchema()},
		})
	}
	return map[string]any{
		"name":               "mhl",
		"description":        "mhl pipelines and workflows exposed as A2A skills.",
		"version":            ServerVersion,
		"protocolVersion":    protocolVersion,
		"url":                s.baseURL,
		"preferredTransport": "JSONRPC",
		"capabilities":       map[string]any{"streaming": false, "pushNotifications": false},
		"defaultInputModes":  []string{"application/json", "text/plain"},
		"defaultOutputModes": []string{"application/json"},
		"skills":             skills,
	}
}

func (s *server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST JSON-RPC only", http.StatusMethodNotAllowed)
		return
	}
	var req rpcReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, rpcErrResp(nil, -32700, "parse error: "+err.Error()))
		return
	}
	switch req.Method {
	case "message/send":
		s.rpcMessageSend(w, r, req)
	case "tasks/get":
		s.rpcTasksGet(w, req)
	case "tasks/cancel":
		s.rpcTasksCancel(w, req)
	default:
		writeJSON(w, http.StatusOK, rpcErrResp(req.ID, -32601, "method not found: "+req.Method))
	}
}

func (s *server) rpcMessageSend(w http.ResponseWriter, r *http.Request, req rpcReq) {
	s.sweep()

	var p struct {
		Message struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"message"`
		Metadata      map[string]any `json:"metadata"`
		Configuration struct {
			Blocking bool `json:"blocking"`
		} `json:"configuration"`
	}
	_ = json.Unmarshal(req.Params, &p)

	meta := p.Message.Metadata
	if meta == nil {
		meta = p.Metadata
	}
	skill, _ := meta["skill"].(string)
	if skill == "" {
		if len(s.workflows) == 1 {
			skill = s.sortedNames()[0]
		} else {
			writeJSON(w, http.StatusOK, rpcErrResp(req.ID, -32602,
				"message.metadata.skill is required; skills: "+joinNames(s.sortedNames())))
			return
		}
	}
	wf, ok := s.workflows[skill]
	if !ok {
		writeJSON(w, http.StatusOK, rpcErrResp(req.ID, -32602, fmt.Sprintf("unknown skill %q", skill)))
		return
	}
	inputs, _ := meta["input"].(map[string]any)

	runCtx, cancel := context.WithCancel(context.Background())
	t := &task{
		id:      runtime.NewSessionID(),
		skill:   skill,
		state:   "submitted",
		created: time.Now(),
		updated: time.Now(),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	s.mu.Lock()
	s.tasks[t.id] = t
	s.mu.Unlock()

	go s.runTask(runCtx, t, wf, inputs)

	// configuration.blocking: hold the response until the task reaches a
	// terminal state, bounded by blockingWait and the client's own
	// connection. Otherwise return the freshly-submitted Task to poll.
	if p.Configuration.Blocking {
		select {
		case <-t.done:
		case <-time.After(blockingWait):
		case <-r.Context().Done():
		}
	}

	writeJSON(w, http.StatusOK, rpcOK(req.ID, s.taskView(t)))
}

func (s *server) runTask(ctx context.Context, t *task, wf execsvc.Workflow, inputs map[string]any) {
	defer t.cancel()
	s.setState(t, "working", "")

	base, err := os.MkdirTemp("", "mhl-a2a-run-")
	if err != nil {
		s.finish(t, nil, "run dir: "+err.Error())
		return
	}
	defer os.RemoveAll(base)

	res, runErr := execsvc.Run(execsvc.Request{
		Context:  ctx,
		Program:  wf.Program,
		File:     wf.File,
		Workflow: wf.Name,
		Inputs:   inputs,
		BaseDir:  base,
	})
	if runErr != nil {
		if ctx.Err() != nil {
			s.setState(t, "canceled", runErr.Error())
			return
		}
		s.finish(t, nil, runErr.Error())
		return
	}
	s.finish(t, res, "")
}

func (s *server) setState(t *task, state, msg string) {
	s.mu.Lock()
	if t.state != "canceled" {
		t.state, t.errMsg, t.updated = state, msg, time.Now()
	}
	terminal := isTerminal(t.state)
	s.mu.Unlock()
	if terminal {
		t.markDone()
	}
}

func (s *server) finish(t *task, res *execsvc.Result, errMsg string) {
	s.mu.Lock()
	if t.state != "canceled" {
		t.result, t.errMsg, t.updated = res, errMsg, time.Now()
		if errMsg != "" {
			t.state = "failed"
		} else {
			t.state = "completed"
		}
	}
	s.mu.Unlock()
	t.markDone()
}

func isTerminal(state string) bool {
	return state == "completed" || state == "failed" || state == "canceled" || state == "rejected"
}

func (s *server) rpcTasksGet(w http.ResponseWriter, req rpcReq) {
	var p struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(req.Params, &p)
	s.mu.Lock()
	t, ok := s.tasks[p.ID]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusOK, rpcErrResp(req.ID, -32001, fmt.Sprintf("task %q not found", p.ID)))
		return
	}
	writeJSON(w, http.StatusOK, rpcOK(req.ID, s.taskView(t)))
}

func (s *server) rpcTasksCancel(w http.ResponseWriter, req rpcReq) {
	var p struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(req.Params, &p)
	s.mu.Lock()
	t, ok := s.tasks[p.ID]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusOK, rpcErrResp(req.ID, -32001, fmt.Sprintf("task %q not found", p.ID)))
		return
	}

	s.mu.Lock()
	terminal := isTerminal(t.state)
	s.mu.Unlock()
	if terminal {
		// A2A TaskNotCancelableError.
		writeJSON(w, http.StatusOK, rpcErrResp(req.ID, -32002,
			fmt.Sprintf("task %q is already in terminal state %q", p.ID, t.state)))
		return
	}

	t.cancel()
	s.mu.Lock()
	if !isTerminal(t.state) {
		t.state, t.updated = "canceled", time.Now()
	}
	s.mu.Unlock()
	t.markDone()
	writeJSON(w, http.StatusOK, rpcOK(req.ID, s.taskView(t)))
}

// taskView renders a task as the A2A Task object.
func (s *server) taskView(t *task) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := map[string]any{
		"state":     t.state,
		"timestamp": t.updated.UTC().Format(time.RFC3339),
	}
	if t.errMsg != "" {
		status["message"] = map[string]any{
			"role":  "agent",
			"parts": []map[string]any{{"kind": "text", "text": t.errMsg}},
		}
	}
	view := map[string]any{
		"id":        t.id,
		"contextId": t.id,
		"kind":      "task",
		"status":    status,
	}
	if t.state == "completed" && t.result != nil {
		text, _ := json.MarshalIndent(t.result.Vars, "", "  ")
		if t.result.Vars == nil {
			text = []byte("{}")
		}
		view["artifacts"] = []map[string]any{{
			"artifactId": "result",
			"name":       "result",
			"parts":      []map[string]any{{"kind": "text", "text": string(text)}},
		}}
	}
	return view
}

func (s *server) sweep() {
	cut := time.Now().Add(-taskTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, t := range s.tasks {
		if t.updated.Before(cut) {
			delete(s.tasks, id)
		}
	}
}

func (s *server) sortedNames() []string {
	names := make([]string, 0, len(s.workflows))
	for n := range s.workflows {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func joinNames(n []string) string {
	out := ""
	for i, s := range n {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("A2A-Version", protocolVersion)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
