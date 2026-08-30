package a2a

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

// ExtensionVersion is this adapter's reported version.
var ExtensionVersion = "0"

// Extension adapts the A2A client to the generic runtime-extension contract.
// It serves the "a2a" declaration kind (`extension a2a X { ... }`).
type Extension struct{}

// NewExtension returns the A2A extension adapter, ready to register.
func NewExtension() extension.Extension { return Extension{} }

func (Extension) ID() string      { return "mhl.a2a" }
func (Extension) Version() string { return ExtensionVersion }

// a2aMethods is the callable surface an a2a extension exposes to `.mh` code —
// single source of truth for the DeclarationSpec and instance.Methods.
var a2aMethods = []extension.MethodSpec{
	{
		Name:          "send",
		Params:        []extension.ParamSpec{{Name: "message", Type: "string"}, {Name: "context", Type: "string", Optional: true}},
		Returns:       "object",
		Signature:     `send(message: string, context?: string) -> object`,
		Documentation: "message/send then poll tasks/get to a terminal state; returns the normalised message or task.",
	},
	{Name: "agent_card", Returns: "object", Signature: "agent_card() -> object", Documentation: "GET /.well-known/agent-card.json — the agent's advertised capabilities."},
	{
		Name:          "get_task",
		Params:        []extension.ParamSpec{{Name: "id", Type: "string"}, {Name: "history_length", Type: "number", Optional: true}},
		Returns:       "object",
		Signature:     "get_task(id: string, history_length?: number) -> object",
		Documentation: "tasks/get for one task id.",
	},
	{Name: "cancel", Params: []extension.ParamSpec{{Name: "id", Type: "string"}}, Returns: "object", Signature: "cancel(id: string) -> object", Documentation: "tasks/cancel for one task id."},
}

func (Extension) Declarations() []extension.DeclarationSpec {
	return []extension.DeclarationSpec{{
		Kind:          "a2a",
		Documentation: "A remote agent that speaks the Agent2Agent (A2A) protocol over JSON-RPC 2.0.",
		Properties: []extension.PropertySpec{
			{Name: "url", Type: "string", Required: true, Documentation: "the agent's JSON-RPC endpoint"},
			{Name: "headers", Type: "object", Documentation: "request headers"},
			{Name: "poll_interval", Type: "duration", Documentation: "delay between tasks/get polls (default 1s)"},
			{Name: "poll_timeout", Type: "duration", Documentation: "give up polling after this (default 120s)"},
		},
		Methods: a2aMethods,
	}}
}

// Validate has no network-free static checks beyond what property evaluation
// already enforces.
func (Extension) Validate(extension.Declaration) []extension.Diagnostic { return nil }

// Bind projects an evaluated declaration onto a Config and registers any
// sensitive header values for redaction, matching the old resolveA2AConfig.
func (Extension) Bind(decl extension.Declaration, host extension.HostContext) (extension.Instance, error) {
	cfg := configFrom(decl)
	for name, value := range cfg.Headers {
		switch http.CanonicalHeaderKey(name) {
		case "Authorization", "Proxy-Authorization", "Cookie":
			auth.Register(value)
		}
	}
	return &instance{name: decl.Name, cfg: cfg, host: host}, nil
}

func configFrom(decl extension.Declaration) Config {
	cfg := Config{Name: decl.Name, Headers: map[string]string{}}
	for _, p := range decl.Props {
		switch p.Name {
		case "url":
			if s, ok := p.Value.(string); ok {
				cfg.URL = s
			}
		case "headers":
			if m, ok := p.Value.(map[string]any); ok {
				for k, v := range m {
					if s, ok := v.(string); ok {
						cfg.Headers[k] = s
					}
				}
			}
		case "poll_interval":
			if d, ok := p.Value.(time.Duration); ok {
				cfg.PollInterval = d
			}
		case "poll_timeout":
			if d, ok := p.Value.(time.Duration); ok {
				cfg.PollTimeout = d
			}
		}
	}
	return cfg
}

type instance struct {
	name string
	cfg  Config
	host extension.HostContext
}

func (i *instance) Methods() []extension.MethodSpec { return a2aMethods }

func (i *instance) Call(_ context.Context, req extension.CallRequest) (extension.Value, error) {
	switch req.Method {
	case "send":
		return i.send(req)
	case "agent_card":
		return i.agentCard(req)
	case "get_task":
		return i.getTask(req)
	case "cancel":
		return i.cancel(req)
	default:
		return nil, fmt.Errorf("a2a %q has no method %q", i.name, req.Method)
	}
}

func (i *instance) send(req extension.CallRequest) (any, error) {
	message, ok := req.StringArg("message", 0)
	if !ok || message == "" {
		return nil, fmt.Errorf("%s.send requires a non-empty string message as its first argument", i.name)
	}
	contextID, _ := req.StringArg("context", 1)

	msg := map[string]any{
		"messageId": newMessageID(),
		"role":      "user",
		"parts":     []any{map[string]any{"kind": "text", "text": message}},
	}
	if contextID != "" {
		msg["contextId"] = contextID
	}
	params := map[string]any{
		"message":       msg,
		"configuration": map[string]any{"blocking": true},
	}

	raw, err := NewClient().SendAndWait(i.cfg, params)
	if err != nil {
		return nil, fmt.Errorf("%s.send: %w", i.name, err)
	}
	return normalizeResult(i.name, "send", raw)
}

func (i *instance) agentCard(req extension.CallRequest) (any, error) {
	if len(req.Args) != 0 || len(req.NamedArgs) != 0 {
		return nil, fmt.Errorf("%s.agent_card takes no arguments", i.name)
	}
	raw, err := NewClient().AgentCard(i.cfg)
	if err != nil {
		return nil, fmt.Errorf("%s.agent_card: %w", i.name, err)
	}
	v, err := nativeops.Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s.agent_card: decoding response: %w", i.name, err)
	}
	return v, nil
}

func (i *instance) getTask(req extension.CallRequest) (any, error) {
	id, ok := req.StringArg("id", 0)
	if !ok || id == "" {
		return nil, fmt.Errorf("%s.get_task requires a string task id as its first argument", i.name)
	}
	historyLength, _ := req.IntArg("history_length", 1)

	raw, err := NewClient().GetTask(i.cfg, id, historyLength)
	if err != nil {
		return nil, fmt.Errorf("%s.get_task: %w", i.name, err)
	}
	return normalizeResult(i.name, "get_task", raw)
}

func (i *instance) cancel(req extension.CallRequest) (any, error) {
	id, ok := req.StringArg("id", 0)
	if !ok || id == "" {
		return nil, fmt.Errorf("%s.cancel requires a string task id as its first argument", i.name)
	}
	raw, err := NewClient().CancelTask(i.cfg, id)
	if err != nil {
		return nil, fmt.Errorf("%s.cancel: %w", i.name, err)
	}
	return normalizeResult(i.name, "cancel", raw)
}

// normalizeResult turns a raw A2A `message/send` or `tasks/get` result into an
// ordinary MHL object, rejecting a task that is waiting on caller input.
func normalizeResult(agentName, op string, raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	kind, state, id := Inspect(raw)
	if IsInterruptedState(state) {
		return nil, fmt.Errorf("%s.%s: remote agent is waiting on additional input (task state %q); mhl's A2A client cannot supply it", agentName, op, state)
	}

	decoded, err := nativeops.Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s.%s: decoding result: %w", agentName, op, err)
	}
	obj, _ := decoded.(map[string]any)
	if obj == nil {
		return decoded, nil
	}

	if kind == "message" {
		return map[string]any{
			"kind":       "message",
			"text":       concatText(obj["parts"]),
			"parts":      obj["parts"],
			"message_id": obj["messageId"],
			"context_id": obj["contextId"],
		}, nil
	}

	var text strings.Builder
	if arts, ok := obj["artifacts"].([]any); ok {
		for _, a := range arts {
			if am, ok := a.(map[string]any); ok {
				appendText(&text, am["parts"])
			}
		}
	}
	if status, ok := obj["status"].(map[string]any); ok {
		if sm, ok := status["message"].(map[string]any); ok {
			appendText(&text, sm["parts"])
		}
	}
	if state == "" {
		if status, ok := obj["status"].(map[string]any); ok {
			state, _ = status["state"].(string)
		}
	}
	taskID := id
	if taskID == "" {
		taskID, _ = obj["id"].(string)
	}
	return map[string]any{
		"kind":       "task",
		"task_id":    taskID,
		"context_id": obj["contextId"],
		"state":      state,
		"text":       strings.TrimSpace(text.String()),
		"artifacts":  obj["artifacts"],
		"history":    obj["history"],
		"status":     obj["status"],
	}, nil
}

func concatText(parts any) string {
	var b strings.Builder
	appendText(&b, parts)
	return strings.TrimSpace(b.String())
}

func appendText(b *strings.Builder, parts any) {
	list, ok := parts.([]any)
	if !ok {
		return
	}
	for _, p := range list {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if t, ok := pm["text"].(string); ok && t != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(t)
		}
	}
}

// newMessageID returns a fresh random id for an outgoing message: 16 random
// bytes hex-encoded, matching the old interpreter helper.
func newMessageID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "mhl-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf[:])
}
