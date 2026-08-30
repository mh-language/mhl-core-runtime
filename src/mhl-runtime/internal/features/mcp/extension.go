package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

// ExtensionVersion is this adapter's reported version. internal/cli may
// overwrite it at init so `mhl version` and the adapter agree.
var ExtensionVersion = "0"

// Extension adapts the MCP client to the generic runtime-extension contract.
// It serves the "mcp" declaration kind (`extension mcp X { ... }`).
type Extension struct{}

// NewExtension returns the MCP extension adapter, ready to register.
func NewExtension() extension.Extension { return Extension{} }

func (Extension) ID() string      { return "mhl.mcp" }
func (Extension) Version() string { return ExtensionVersion }

// mcpMethods is the callable surface an mcp extension exposes to `.mh` code. It
// is the single source of truth: the DeclarationSpec (for lint / LSP) and
// instance.Methods both read it.
var mcpMethods = []extension.MethodSpec{
	{
		Name:          "call",
		Params:        []extension.ParamSpec{{Name: "tool", Type: "string"}, {Name: "arguments", Type: "object", Optional: true}},
		Returns:       "any",
		Signature:     `call(tool: string, arguments?: object) -> any`,
		Documentation: "Invoke a tool by name via JSON-RPC tools/call; returns the decoded result.",
	},
	{Name: "list_tools", Returns: "object", Signature: "list_tools() -> object", Documentation: "Real-time tools/list discovery of the server's catalogue."},
	{Name: "discover", Returns: "object", Signature: "discover() -> object", Documentation: "server/discover: the server's advertised versions, capabilities and identity."},
}

func (Extension) Declarations() []extension.DeclarationSpec {
	return []extension.DeclarationSpec{{
		Kind:          "mcp",
		Documentation: "A Model Context Protocol server: stateless JSON-RPC over stdio or HTTP.",
		Properties: []extension.PropertySpec{
			{Name: "transport", Type: "string", Documentation: `"stdio" or "http"`},
			{Name: "command", Type: "string", Documentation: "stdio: the server executable"},
			{Name: "args", Type: "string[]", Documentation: "stdio: arguments for command"},
			{Name: "url", Type: "string", Documentation: "http: the server endpoint"},
			{Name: "headers", Type: "object", Documentation: "http: request headers"},
			{Name: "protocol", Type: "string", Documentation: `"auto" (default), "2026-07-28", "2025-11-25", or "2025-06-18"`},
		},
		Methods: mcpMethods,
	}}
}

// Validate performs the one static check the old lint path did:
// a `protocol:` value must be one ParseProtocol recognises.
func (Extension) Validate(decl extension.Declaration) []extension.Diagnostic {
	if _, err := serverConfigFrom(decl); err != nil {
		return []extension.Diagnostic{extension.Errorf(decl.Pos, "mcp-invalid-config", "%s", err.Error())}
	}
	return nil
}

// Bind resolves decl into a ServerConfig and returns a stateless instance.
func (Extension) Bind(decl extension.Declaration, host extension.HostContext) (extension.Instance, error) {
	cfg, err := serverConfigFrom(decl)
	if err != nil {
		return nil, err
	}
	return &instance{name: decl.Name, cfg: cfg, host: host}, nil
}

// serverConfigFrom projects an already-evaluated declaration onto a
// ServerConfig. It replaces registry.serverFromAST's literal readers: the
// interpreter has already evaluated every property value.
func serverConfigFrom(decl extension.Declaration) (ServerConfig, error) {
	cfg := ServerConfig{Name: decl.Name, Headers: map[string]string{}}
	for _, p := range decl.Props {
		switch p.Name {
		case "transport":
			if s, ok := p.Value.(string); ok {
				cfg.Transport = Transport(s)
			}
		case "command":
			if s, ok := p.Value.(string); ok {
				cfg.Command = s
			}
		case "url":
			if s, ok := p.Value.(string); ok {
				cfg.URL = s
			}
		case "args":
			cfg.Args = toStringSlice(p.Value)
		case "headers":
			cfg.Headers = toStringMap(p.Value)
		case "protocol":
			s, _ := p.Value.(string)
			proto, err := ParseProtocol(s)
			if err != nil {
				// Name the declaration in the message, like
				// ast.MCPProtocolFromProps does for the lint path.
				return ServerConfig{}, fmt.Errorf(
					"mcp %q protocol %q is not supported — use %q, %q, %q, or %q",
					decl.Name, s, ProtocolAuto, ProtocolStateless, ProtocolHandshake2511, ProtocolHandshake2506)
			}
			cfg.Protocol = proto
		}
	}
	return cfg, nil
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toStringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	out := map[string]string{}
	if !ok {
		return out
	}
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

// instance is one bound mcp extension. It keeps no session between calls — that
// statelessness is a property of the mcp package itself.
type instance struct {
	name string
	cfg  ServerConfig
	host extension.HostContext
}

func (i *instance) Methods() []extension.MethodSpec { return mcpMethods }

func (i *instance) Call(_ context.Context, req extension.CallRequest) (extension.Value, error) {
	switch req.Method {
	case "call":
		return i.toolCall(req)
	case "list_tools":
		if len(req.Args) != 0 || len(req.NamedArgs) != 0 {
			return nil, fmt.Errorf("%s.list_tools takes no arguments", i.name)
		}
		return i.issue("list_tools", ToolRequest{Method: "tools/list"})
	case "discover":
		if len(req.Args) != 0 || len(req.NamedArgs) != 0 {
			return nil, fmt.Errorf("%s.discover takes no arguments", i.name)
		}
		return i.issue("discover", ToolRequest{Method: "server/discover"})
	default:
		return nil, fmt.Errorf("mcp %q has no method %q", i.name, req.Method)
	}
}

func (i *instance) toolCall(req extension.CallRequest) (any, error) {
	toolName, ok := req.StringArg("tool", 0)
	if !ok {
		return nil, fmt.Errorf("%s.call requires a string tool name as its first argument", i.name)
	}

	toolArgs := map[string]any{}
	if obj, isObj, supplied := req.ObjectArg("arguments", 1); supplied {
		if !isObj {
			if _, named := req.NamedArgs["arguments"]; named {
				return nil, fmt.Errorf("%s.call: arguments must be an object", i.name)
			}
			return nil, fmt.Errorf("%s.call: second argument must be an object of tool arguments", i.name)
		}
		toolArgs = obj
	}

	// x-mcp-header (spec 2026-07-28) only applies to the http transport.
	// Best-effort: an unresolvable schema just means the call proceeds
	// without those headers.
	var paramHeaders map[string]string
	if i.cfg.Transport == TransportHTTP {
		paramHeaders = i.xMCPHeaderParams(toolName, toolArgs)
	}

	return i.issue("call", ToolRequest{
		Method:       "tools/call",
		Params:       map[string]any{"name": toolName, "arguments": toolArgs},
		ParamHeaders: paramHeaders,
	})
}

// issue sends request against the bound config and decodes a successful
// result's raw JSON back into an ordinary MHL value. op names the calling
// `.mh` method, only for error messages.
func (i *instance) issue(op string, request ToolRequest) (any, error) {
	result, err := NewClient().CallTool(i.cfg, request)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", i.name, op, err)
	}
	if result.IsInputRequired() {
		return nil, fmt.Errorf("%s.%s: server requires additional input (elicitation/sampling) to complete this call; mhl's MCP client does not support Multi Round-Trip Requests", i.name, op)
	}
	if len(result.Raw) == 0 {
		return nil, nil
	}
	v, err := nativeops.Parse(string(result.Raw))
	if err != nil {
		return nil, fmt.Errorf("%s.%s: decoding result: %w", i.name, op, err)
	}
	return v, nil
}

// xMCPHeaderParams fetches toolName's inputSchema via a "tools/list" call,
// extracts its top-level x-mcp-header annotations, and formats each annotated
// argument present in toolArgs. Returns nil on any failure to resolve the
// schema — a server that genuinely requires the headers rejects the call
// with its own error, a clearer failure than this client guessing.
func (i *instance) xMCPHeaderParams(toolName string, toolArgs map[string]any) map[string]string {
	listResult, err := NewClient().CallTool(i.cfg, ToolRequest{Method: "tools/list"})
	if err != nil || len(listResult.Raw) == 0 {
		return nil
	}
	var decoded struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listResult.Raw, &decoded); err != nil {
		return nil
	}
	var schema map[string]any
	for _, t := range decoded.Tools {
		if t.Name == toolName {
			schema = t.InputSchema
			break
		}
	}
	if schema == nil {
		return nil
	}

	headers := map[string]string{}
	for argName, headerName := range xMCPHeaderNames(schema) {
		v, present := toolArgs[argName]
		if !present || v == nil {
			continue
		}
		if s, ok := formatHeaderParamValue(v); ok {
			headers[headerName] = s
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// xMCPHeaderNames reads inputSchema's top-level `properties` for
// `x-mcp-header` annotations on string/integer/boolean fields.
func xMCPHeaderNames(inputSchema map[string]any) map[string]string {
	props, _ := inputSchema["properties"].(map[string]any)
	out := map[string]string{}
	for propName, raw := range props {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		headerName, ok := prop["x-mcp-header"].(string)
		if !ok || headerName == "" {
			continue
		}
		switch prop["type"] {
		case "string", "integer", "boolean":
			out[propName] = headerName
		}
	}
	return out
}

// formatHeaderParamValue converts an evaluated MHL argument into its string
// form per spec 2026-07-28's Value Encoding rules.
func formatHeaderParamValue(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	case float64:
		n := int64(t)
		if float64(n) != t {
			return "", false
		}
		return strconv.FormatInt(n, 10), true
	default:
		return "", false
	}
}
