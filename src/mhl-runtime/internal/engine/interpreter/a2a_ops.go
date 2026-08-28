package interpreter

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/a2a"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

func findA2AAgent(prog *ast.Program, name string) (*ast.A2AAgent, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.A2AAgent != nil && decl.A2AAgent.Name == name {
			return decl.A2AAgent, true
		}
	}
	return nil, false
}

// evalA2AAgentCall dispatches the operations a declared a2a_agent exposes to
// `.mh` code: `<Agent>.send("...")`, `<Agent>.agent_card()`,
// `<Agent>.get_task(id)`, and `<Agent>.cancel(id)`. It mirrors
// evalMCPServerCall (mcp_ops.go): the config is resolved fail-closed through
// features/a2a.BuildRegistryWithError so an unresolved env(...) credential
// aborts before any request, and every call is a fresh, stateless
// JSON-RPC 2.0 request (or, for agent_card, a plain GET) through
// features/a2a.Client — no session is kept between calls. `.send` may issue
// several requests (a `message/send` followed by `tasks/get` polling) but
// carries no state beyond the task id.
func evalA2AAgentCall(ctx *evalCtx, agent *ast.A2AAgent, member string, call *ast.Call, depth int) (any, error) {
	switch member {
	case "send":
		return evalA2ASend(ctx, agent, call, depth)
	case "agent_card":
		return evalA2AAgentCard(ctx, agent, call, depth)
	case "get_task":
		return evalA2AGetTask(ctx, agent, call, depth)
	case "cancel":
		return evalA2ACancel(ctx, agent, call, depth)
	default:
		return nil, fmt.Errorf("a2a_agent %q has no method %q", agent.Name, member)
	}
}

func evalA2ASend(ctx *evalCtx, agent *ast.A2AAgent, call *ast.Call, depth int) (any, error) {
	args, err := evalCallArgs(ctx, call, depth)
	if err != nil {
		return nil, err
	}
	message, ok := args.stringNamedOrAt("message", 0)
	if !ok || message == "" {
		return nil, fmt.Errorf("%s.send requires a non-empty string message as its first argument", agent.Name)
	}
	contextID, _ := args.stringNamedOrAt("context", 1)

	cfg, err := resolveA2AConfig(ctx, agent, "send")
	if err != nil {
		return nil, err
	}

	msg := map[string]any{
		"messageId": newA2AMessageID(),
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

	raw, err := a2a.NewClient().SendAndWait(cfg, params)
	if err != nil {
		return nil, fmt.Errorf("%s.send: %w", agent.Name, err)
	}
	return normalizeA2AResult(agent.Name, "send", raw)
}

func evalA2AAgentCard(ctx *evalCtx, agent *ast.A2AAgent, call *ast.Call, depth int) (any, error) {
	if len(call.Args) != 0 {
		return nil, fmt.Errorf("%s.agent_card takes no arguments", agent.Name)
	}
	cfg, err := resolveA2AConfig(ctx, agent, "agent_card")
	if err != nil {
		return nil, err
	}
	raw, err := a2a.NewClient().AgentCard(cfg)
	if err != nil {
		return nil, fmt.Errorf("%s.agent_card: %w", agent.Name, err)
	}
	v, err := nativeops.Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s.agent_card: decoding response: %w", agent.Name, err)
	}
	return v, nil
}

func evalA2AGetTask(ctx *evalCtx, agent *ast.A2AAgent, call *ast.Call, depth int) (any, error) {
	args, err := evalCallArgs(ctx, call, depth)
	if err != nil {
		return nil, err
	}
	id, ok := args.stringNamedOrAt("id", 0)
	if !ok || id == "" {
		return nil, fmt.Errorf("%s.get_task requires a string task id as its first argument", agent.Name)
	}
	historyLength, _ := args.intNamedOrAt("history_length", 1)

	cfg, err := resolveA2AConfig(ctx, agent, "get_task")
	if err != nil {
		return nil, err
	}
	raw, err := a2a.NewClient().GetTask(cfg, id, historyLength)
	if err != nil {
		return nil, fmt.Errorf("%s.get_task: %w", agent.Name, err)
	}
	return normalizeA2AResult(agent.Name, "get_task", raw)
}

func evalA2ACancel(ctx *evalCtx, agent *ast.A2AAgent, call *ast.Call, depth int) (any, error) {
	args, err := evalCallArgs(ctx, call, depth)
	if err != nil {
		return nil, err
	}
	id, ok := args.stringNamedOrAt("id", 0)
	if !ok || id == "" {
		return nil, fmt.Errorf("%s.cancel requires a string task id as its first argument", agent.Name)
	}
	cfg, err := resolveA2AConfig(ctx, agent, "cancel")
	if err != nil {
		return nil, err
	}
	raw, err := a2a.NewClient().CancelTask(cfg, id)
	if err != nil {
		return nil, fmt.Errorf("%s.cancel: %w", agent.Name, err)
	}
	return normalizeA2AResult(agent.Name, "cancel", raw)
}

// resolveA2AConfig resolves agent's config, fail-closed on any unresolved
// credential (env(...) that isn't set) or undeclared agent, and registers
// sensitive header values so they are redacted from traced/persisted output
// (matching interpreter/tool.go's handling of http.* headers). op names the
// calling `.mh` method, only for error messages.
func resolveA2AConfig(ctx *evalCtx, agent *ast.A2AAgent, op string) (a2a.Config, error) {
	registry, err := a2a.BuildRegistryWithError(ctx.prog)
	if err != nil {
		return a2a.Config{}, fmt.Errorf("%s.%s: %w", agent.Name, op, err)
	}
	cfg, ok := registry.Get(agent.Name)
	if !ok {
		return a2a.Config{}, fmt.Errorf("%s.%s: a2a_agent %q is not declared", agent.Name, op, agent.Name)
	}
	for name, value := range cfg.Headers {
		switch http.CanonicalHeaderKey(name) {
		case "Authorization", "Proxy-Authorization", "Cookie":
			auth.Register(value)
		}
	}
	return cfg, nil
}

// normalizeA2AResult turns a raw A2A `message/send` or `tasks/get` result into
// an ordinary MHL object. A Message becomes
// `{kind, text, parts, message_id, context_id}`; a Task becomes
// `{kind, task_id, context_id, state, text, artifacts, history, status}`,
// where `text` is the concatenation of every text part (a convenience — the
// raw `parts`/`artifacts` are still present). A task waiting on caller input
// (`input-required`/`auth-required`) is rejected outright: mhl's A2A client
// has no way to gather and resubmit that input, so returning the interim
// shape as if it were the answer would hand the caller garbage.
func normalizeA2AResult(agentName, op string, raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	kind, state, id := a2a.Inspect(raw)
	if a2a.IsInterruptedState(state) {
		return nil, fmt.Errorf("%s.%s: remote agent is waiting on additional input (task state %q); mhl's A2A client cannot supply it", agentName, op, state)
	}

	decoded, err := nativeops.Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s.%s: decoding result: %w", agentName, op, err)
	}
	obj, _ := decoded.(map[string]any)
	if obj == nil {
		// Not an object we recognise — hand back the decoded value as-is.
		return decoded, nil
	}

	if kind == "message" {
		return map[string]any{
			"kind":       "message",
			"text":       concatA2AText(obj["parts"]),
			"parts":      obj["parts"],
			"message_id": obj["messageId"],
			"context_id": obj["contextId"],
		}, nil
	}

	// Task (or an unrecognised shape treated as one).
	var text strings.Builder
	if arts, ok := obj["artifacts"].([]any); ok {
		for _, a := range arts {
			if am, ok := a.(map[string]any); ok {
				appendA2AText(&text, am["parts"])
			}
		}
	}
	if status, ok := obj["status"].(map[string]any); ok {
		if sm, ok := status["message"].(map[string]any); ok {
			appendA2AText(&text, sm["parts"])
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

// concatA2AText joins the `text` fields of every text-kind part in parts.
func concatA2AText(parts any) string {
	var b strings.Builder
	appendA2AText(&b, parts)
	return strings.TrimSpace(b.String())
}

func appendA2AText(b *strings.Builder, parts any) {
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

// newA2AMessageID returns a fresh random id for an outgoing message. Like
// runtime.NewSessionID it is 16 random bytes hex-encoded rather than a UUID,
// to avoid pulling in a UUID dependency for a value the protocol only needs
// to be unique.
func newA2AMessageID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "mhl-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf[:])
}
