package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultTimeout      = 60 * time.Second
	defaultPollInterval = 1 * time.Second
	defaultPollTimeout  = 120 * time.Second
	wellKnownCardPath   = "/.well-known/agent-card.json"
)

// terminalStates are the A2A 0.2.x TaskState values that end a task; a
// blocking send stops polling once the task reaches one of them.
var terminalStates = map[string]bool{
	"completed": true,
	"canceled":  true,
	"failed":    true,
	"rejected":  true,
}

// interruptedStates are the TaskState values that need caller-supplied input
// this client cannot provide. A send that lands on one of them is surfaced to
// the caller (a2a_ops.go rejects it with a clear error), not polled further.
var interruptedStates = map[string]bool{
	"input-required": true,
	"auth-required":  true,
}

// IsTerminalState reports whether state ends a task.
func IsTerminalState(state string) bool { return terminalStates[state] }

// IsInterruptedState reports whether state means the agent is waiting on
// input this client cannot supply.
func IsInterruptedState(state string) bool { return interruptedStates[state] }

// Config is the resolved configuration of a declared a2a_agent. Any
// credential values in Headers are already resolved by the caller; this
// package only consumes resolved values and never stores credentials.
type Config struct {
	Name         string
	URL          string
	Headers      map[string]string
	PollInterval time.Duration
	PollTimeout  time.Duration
}

func (c Config) pollInterval() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return defaultPollInterval
}

func (c Config) pollTimeout() time.Duration {
	if c.PollTimeout > 0 {
		return c.PollTimeout
	}
	return defaultPollTimeout
}

// Client is a stateless A2A client. A single Client may be reused across
// agents and calls; it holds no per-agent session state.
type Client struct {
	// HTTPClient is used for every request. If nil, a default client with a
	// sane timeout is used.
	HTTPClient *http.Client
}

// NewClient returns a Client with default settings.
func NewClient() *Client {
	return &Client{HTTPClient: &http.Client{Timeout: defaultTimeout}}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: defaultTimeout}
}

// Inspect peeks at a raw `message/send` or `tasks/get` result and classifies
// it as "message" or "task" without fully decoding. For a task it also
// returns the current state and id.
func Inspect(raw json.RawMessage) (kind, state, id string) {
	var probe struct {
		Kind   string `json:"kind"`
		ID     string `json:"id"`
		Role   string `json:"role"`
		Status struct {
			State string `json:"state"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", "", ""
	}
	switch {
	case probe.Kind == "message" || probe.Role != "":
		return "message", "", ""
	case probe.Kind == "task" || probe.Status.State != "" || probe.ID != "":
		return "task", probe.Status.State, probe.ID
	default:
		return "", "", ""
	}
}

// SendMessage issues one `message/send` and returns the raw JSON-RPC
// `result` — either a Message object or a Task object.
func (c *Client) SendMessage(cfg Config, params any) (json.RawMessage, error) {
	return c.rpcCall(cfg, "message/send", params)
}

// SendAndWait issues `message/send`; when the result is a Task that is not
// yet terminal (or interrupted), it polls `tasks/get` every PollInterval
// until the task reaches a terminal or interrupted state, or PollTimeout
// elapses. A Message result is returned as-is with no polling.
func (c *Client) SendAndWait(cfg Config, params any) (json.RawMessage, error) {
	raw, err := c.SendMessage(cfg, params)
	if err != nil {
		return nil, err
	}
	kind, state, id := Inspect(raw)
	if kind != "task" || id == "" {
		return raw, nil
	}
	if terminalStates[state] || interruptedStates[state] {
		return raw, nil
	}

	deadline := time.Now().Add(cfg.pollTimeout())
	for {
		time.Sleep(cfg.pollInterval())
		raw, err = c.GetTask(cfg, id, 0)
		if err != nil {
			return nil, err
		}
		if _, state, _ = Inspect(raw); terminalStates[state] || interruptedStates[state] {
			return raw, nil
		}
		if time.Now().After(deadline) {
			return nil, &A2AError{
				Agent:   cfg.Name,
				Method:  "tasks/get",
				Message: fmt.Sprintf("task %s did not reach a terminal state within %s (last state %q)", id, cfg.pollTimeout(), state),
			}
		}
	}
}

// GetTask issues one `tasks/get`. A historyLength <= 0 omits the field.
func (c *Client) GetTask(cfg Config, id string, historyLength int) (json.RawMessage, error) {
	params := map[string]any{"id": id}
	if historyLength > 0 {
		params["historyLength"] = historyLength
	}
	return c.rpcCall(cfg, "tasks/get", params)
}

// CancelTask issues one `tasks/cancel`.
func (c *Client) CancelTask(cfg Config, id string) (json.RawMessage, error) {
	return c.rpcCall(cfg, "tasks/cancel", map[string]any{"id": id})
}

// AgentCard fetches the public Agent Card. The card lives at a well-known
// path on the agent's origin, so the configured URL's own path (e.g. "/a2a")
// is replaced with "/.well-known/agent-card.json".
func (c *Client) AgentCard(cfg Config) (json.RawMessage, error) {
	base, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: "agent_card", Message: "invalid url", Err: err}
	}
	base.Path = wellKnownCardPath
	base.RawQuery = ""
	base.Fragment = ""

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: "agent_card", Message: "building request", Err: err}
	}
	httpReq.Header.Set("Accept", "application/json")
	for k, v := range cfg.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: "agent_card", Message: "request failed", Err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: "agent_card", Message: "reading response body", Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &A2AError{Agent: cfg.Name, Method: "agent_card", Code: resp.StatusCode, Message: fmt.Sprintf("HTTP %s", resp.Status)}
	}
	if !json.Valid(bytes.TrimSpace(body)) {
		return nil, &A2AError{Agent: cfg.Name, Method: "agent_card", Message: "response body is not valid JSON"}
	}
	return json.RawMessage(bytes.TrimSpace(body)), nil
}

// rpcCall issues a single stateless JSON-RPC 2.0 POST carrying method+params
// and returns the decoded `result`, or a typed A2AError.
func (c *Client) rpcCall(cfg Config, method string, params any) (json.RawMessage, error) {
	if cfg.URL == "" {
		return nil, &A2AError{Agent: cfg.Name, Method: method, Message: "no url configured"}
	}

	var rawParams json.RawMessage
	if params != nil {
		p, err := json.Marshal(params)
		if err != nil {
			return nil, &A2AError{Agent: cfg.Name, Method: method, Message: "encoding params", Err: err}
		}
		rawParams = p
	}
	reqBytes, err := json.Marshal(Request{JSONRPC: JSONRPCVersion, ID: 1, Method: method, Params: rawParams})
	if err != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: method, Message: "encoding request", Err: err}
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: method, Message: "building request", Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	for k, v := range cfg.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: method, Message: "request failed", Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: method, Message: "reading response body", Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &A2AError{Agent: cfg.Name, Method: method, Code: resp.StatusCode, Message: fmt.Sprintf("HTTP %s", resp.Status)}
	}

	var rpcResp Response
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: method, Message: "malformed JSON-RPC response", Err: err}
	}
	if rpcResp.Error != nil {
		return nil, &A2AError{Agent: cfg.Name, Method: method, Code: rpcResp.Error.Code, Message: rpcResp.Error.Message}
	}
	return rpcResp.Result, nil
}
