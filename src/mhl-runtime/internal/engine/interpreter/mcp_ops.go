package interpreter

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/mh-language/mhl-core-runtime/internal/features/mcp"
	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

func findMCPServer(prog *ast.Program, name string) (*ast.MCPServer, bool) {
	name = resolveName(prog, name)
	for _, decl := range prog.Decls {
		if decl.MCPServer != nil && decl.MCPServer.Name == name {
			return decl.MCPServer, true
		}
	}
	return nil, false
}

// evalMCPServerCall dispatches the operations a declared mcp_server exposes
// to `.mh` code: `<Server>.call("toolName", { ... })`, `<Server>.
// list_tools()`, and `<Server>.discover()`, mirroring how a declared
// `tool`'s methods are called by name (see evalToolCall in tool.go). All
// three resolve the server's config through features/mcp.
// BuildRegistryWithError — fail-closed, so an unresolved env(...) credential
// (a missing GITHUB_TOKEN, say) aborts the call instead of silently sending
// a request with an empty Authorization header — and issue exactly one
// stateless JSON-RPC request through features/mcp.Client. No connection or
// session is kept between calls; that statelessness is a property of the
// mcp package itself, not something added here.
func evalMCPServerCall(ctx *evalCtx, server *ast.MCPServer, member string, call *ast.Call, depth int) (any, error) {
	switch member {
	case "call":
		return evalMCPServerToolCall(ctx, server, call, depth)
	case "list_tools":
		return evalMCPServerListTools(ctx, server, call, depth)
	case "discover":
		return evalMCPServerDiscover(ctx, server, call, depth)
	default:
		return nil, fmt.Errorf("mcp_server %q has no method %q", server.Name, member)
	}
}

func evalMCPServerToolCall(ctx *evalCtx, server *ast.MCPServer, call *ast.Call, depth int) (any, error) {
	args, err := evalCallArgs(ctx, call, depth)
	if err != nil {
		return nil, err
	}
	toolName, ok := args.stringNamedOrAt("tool", 0)
	if !ok {
		return nil, fmt.Errorf("%s.call requires a string tool name as its first argument", server.Name)
	}

	toolArgs := map[string]any{}
	if v, hasNamed := args.named["arguments"]; hasNamed {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.call: arguments must be an object", server.Name)
		}
		toolArgs = m
	} else if len(args.positional) > 1 {
		m, ok := args.positional[1].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.call: second argument must be an object of tool arguments", server.Name)
		}
		toolArgs = m
	}

	cfg, err := resolveMCPServerConfig(ctx, server, "call")
	if err != nil {
		return nil, err
	}

	// x-mcp-header (spec 2026-07-28) only applies to the http transport —
	// stdio has no headers to mirror into, matching the spec's own scoping
	// ("Clients using other transports ... MAY ignore x-mcp-header
	// annotations entirely"). Best-effort: if the schema can't be fetched
	// (network hiccup, server doesn't implement tools/list, tool not
	// found), the call still proceeds without those headers — a server
	// that genuinely required them rejects the request with its own
	// HeaderMismatch error, a real and clear failure rather than this
	// client silently guessing.
	var paramHeaders map[string]string
	if cfg.Transport == mcp.TransportHTTP {
		paramHeaders = xMCPHeaderParamsFor(cfg, toolName, toolArgs)
	}

	return issueMCPServerCall(server, "call", cfg, mcp.ToolRequest{
		Method:       "tools/call",
		Params:       map[string]any{"name": toolName, "arguments": toolArgs},
		ParamHeaders: paramHeaders,
	})
}

// xMCPHeaderParamsFor fetches toolName's inputSchema via a "tools/list" call
// against cfg, extracts its top-level x-mcp-header annotations
// (xMCPHeaderParams), and formats each annotated argument actually present
// in toolArgs into the raw (not yet header-encoded) value
// ToolRequest.ParamHeaders expects. Returns nil on any failure to resolve
// the schema — see the caller's doc comment for why that's the right
// fallback here, not an error.
func xMCPHeaderParamsFor(cfg mcp.ServerConfig, toolName string, toolArgs map[string]any) map[string]string {
	listResult, err := mcp.NewClient().CallTool(cfg, mcp.ToolRequest{Method: "tools/list"})
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
	for argName, headerName := range xMCPHeaderParams(schema) {
		v, present := toolArgs[argName]
		if !present || v == nil {
			continue // spec: absent/null value -> the header is omitted, not sent empty
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

// xMCPHeaderParams reads inputSchema's top-level `properties` for
// `x-mcp-header` annotations, returning an argument-name -> header-name
// map restricted to spec 2026-07-28's allowed primitive types (string,
// integer, boolean — not number/array/object). This is a v1 simplification
// of the spec's fuller allowance (a property reachable through any chain
// of nested `properties` keys, not just top-level) — the common case
// (language-design.md's own execute_sql example) is top-level.
func xMCPHeaderParams(inputSchema map[string]any) map[string]string {
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

// formatHeaderParamValue converts an already-evaluated MHL argument value
// into its string form per spec 2026-07-28's Value Encoding "type
// conversion" rules: string as-is, integer as decimal, boolean as lowercase
// true/false. ok is false for anything else (including a float64 that
// isn't an exact integer — mhl has one numeric runtime type, so this is how
// a "number"-typed argument that happens not to be schema-eligible anyway
// is told apart from a genuine integer).
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

// evalMCPServerListTools issues a JSON-RPC "tools/list" — real-time
// discovery of what a declared mcp_server actually offers, in place of the
// caller having to already know a tool's exact name and argument shape
// before ever calling it. This is deliberately a runtime operation, not
// something `mhl lint` calls: lint never touches the network (nothing else
// it checks does either — an agent's `command:` binary isn't verified to
// exist), and there is no persistent session here to cache a discovered
// tool list against between lint and run. A single page is fetched; a
// server that paginates its tool list (an optional `nextCursor` in the
// spec) has any tools past the first page silently omitted rather than
// followed — most servers don't paginate a handful of tools, but a large
// catalog would need that added.
func evalMCPServerListTools(ctx *evalCtx, server *ast.MCPServer, call *ast.Call, depth int) (any, error) {
	if len(call.Args) != 0 {
		return nil, fmt.Errorf("%s.list_tools takes no arguments", server.Name)
	}
	return callMCPServer(ctx, server, "list_tools", mcp.ToolRequest{Method: "tools/list"})
}

// evalMCPServerDiscover issues a JSON-RPC "server/discover" — spec
// 2026-07-28 requires every server to implement it, advertising its
// supported protocol versions, capabilities, and identity. A client MAY
// call it before any other request for up-front version selection; this
// client doesn't (it always sends the one version it speaks, SpecVersion,
// and lets a real mismatch surface as an UnsupportedProtocolVersionError
// from the call itself — see versioning.md's negotiation flow), so
// `.discover()` exists here purely as a real, callable primitive for a
// `.mh` script that wants to inspect a server before deciding whether/how
// to use it, not something mhl calls on its own.
func evalMCPServerDiscover(ctx *evalCtx, server *ast.MCPServer, call *ast.Call, depth int) (any, error) {
	if len(call.Args) != 0 {
		return nil, fmt.Errorf("%s.discover takes no arguments", server.Name)
	}
	return callMCPServer(ctx, server, "discover", mcp.ToolRequest{Method: "server/discover"})
}

// resolveMCPServerConfig resolves server's config, fail-closed on any
// unresolved credential (env(...) that isn't set) or undeclared server. op
// names the calling `.mh` method, only for error messages.
func resolveMCPServerConfig(ctx *evalCtx, server *ast.MCPServer, op string) (mcp.ServerConfig, error) {
	registry, err := mcp.BuildRegistryWithError(ctx.prog)
	if err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("%s.%s: %w", server.Name, op, err)
	}
	cfg, ok := registry.Get(server.Name)
	if !ok {
		return mcp.ServerConfig{}, fmt.Errorf("%s.%s: mcp_server %q is not declared", server.Name, op, server.Name)
	}
	return cfg, nil
}

// issueMCPServerCall issues request against cfg and decodes a successful
// result's raw JSON back into an ordinary MHL value. op names the calling
// `.mh` method, only for error messages. A "input_required" resultType
// (spec 2026-07-28's Multi Round-Trip Requests pattern — the server needs
// elicitation/sampling input before it can finish) is rejected outright:
// this client has no way to gather and resubmit `inputResponses`, so
// returning the InputRequiredResult's shape as if it were the tool's real
// data would silently hand the caller garbage instead of a clear failure.
func issueMCPServerCall(server *ast.MCPServer, op string, cfg mcp.ServerConfig, request mcp.ToolRequest) (any, error) {
	result, err := mcp.NewClient().CallTool(cfg, request)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", server.Name, op, err)
	}
	if result.IsInputRequired() {
		return nil, fmt.Errorf("%s.%s: server requires additional input (elicitation/sampling) to complete this call; mhl's MCP client does not support Multi Round-Trip Requests", server.Name, op)
	}
	if len(result.Raw) == 0 {
		return nil, nil
	}
	v, err := nativeops.Parse(string(result.Raw))
	if err != nil {
		return nil, fmt.Errorf("%s.%s: decoding result: %w", server.Name, op, err)
	}
	return v, nil
}

// callMCPServer resolves server's config and issues request in one step —
// what list_tools/discover use, since neither needs the config for
// anything else first (unlike evalMCPServerToolCall, which inspects
// cfg.Transport before deciding whether to attempt x-mcp-header).
func callMCPServer(ctx *evalCtx, server *ast.MCPServer, op string, request mcp.ToolRequest) (any, error) {
	cfg, err := resolveMCPServerConfig(ctx, server, op)
	if err != nil {
		return nil, err
	}
	return issueMCPServerCall(server, op, cfg, request)
}
