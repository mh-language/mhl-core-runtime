package interpreter

import (
	"fmt"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// agentToolScope reads an agent's `tools: [...]` / `mcp_servers: [...]`
// properties — a list of bare tool names, dotted tool.method references
// (e.g. `execution.read_file`), or bare mcp_server names.
func agentToolScope(agent *ast.Agent) (tools, mcpServers []string) {
	for _, p := range agent.Props {
		switch p.Name {
		case "tools":
			tools = refListValue(p.Value)
		case "mcp_servers":
			mcpServers = refListValue(p.Value)
		}
	}
	return tools, mcpServers
}

// refListValue flattens an array-literal of tool/mcp_server references
// (e.g. `[execution.read_file, execution.git_diff]` or `[GitHub]`) into
// their dotted-name string form. Entries that are not simple references are
// skipped.
func refListValue(e *ast.Expr) []string {
	arr := ast.BareArray(e)
	if arr == nil {
		return nil
	}
	var out []string
	for _, item := range arr.Items {
		if name, ok := refName(item); ok {
			out = append(out, name)
		}
	}
	return out
}

// refName renders a simple dotted reference expression (an identifier
// followed only by `.member` accesses, e.g. `execution.read_file`) into its
// string form. It returns false for anything more complex.
func refName(e *ast.Expr) (string, bool) {
	pf := ast.BarePostfix(e)
	if pf == nil || pf.Primary == nil || pf.Primary.Ident == "" {
		return "", false
	}
	name := pf.Primary.Ident
	for _, t := range pf.Ops {
		if t.Member == "" {
			return "", false
		}
		name += "." + t.Member
	}
	return name, true
}

// applyAgentDeclaredToolScope folds an agent's own `tools:`/`mcp_servers:`
// properties into promptText when it declares either, so a `.run()` call
// tells the model what it may use instead of those two properties parsing
// and doing nothing. An agent declaring neither leaves promptText
// untouched.
//
// Architecture note: mhl agents are CLI subprocesses (runAgentAttempt) —
// mhl has no structural channel to force the underlying model to use only
// certain tools, the way a real tool-calling API's tool list would. What IS
// enforced here, by refusing to run rather than degrading silently: every
// name in scope must resolve to a real declaration (validateDeclaredScope)
// — a typo in `tools:`/`mcp_servers:` aborts the call instead of silently
// narrowing scope to nothing. The restriction itself is unavoidably
// best-effort beyond that: it's written into the prompt as an explicit
// instruction to the model, not something mhl's own process boundary can
// enforce against a CLI-backed agent.
func applyAgentDeclaredToolScope(ctx *evalCtx, agent *ast.Agent, promptText string) (string, error) {
	tools, mcpServers := agentToolScope(agent)
	if len(tools) == 0 && len(mcpServers) == 0 {
		return promptText, nil
	}
	if err := validateDeclaredScope(ctx.prog, tools, mcpServers); err != nil {
		return "", fmt.Errorf("agent %q: %w", agent.Name, err)
	}
	return buildScopedPrompt(tools, mcpServers, promptText), nil
}

// validateDeclaredScope fails closed when a name in tools/mcpServers isn't
// actually declared in the program — without this, a typo in a `tools:`/
// `mcp_servers:` list would silently narrow the invocation's scope to
// nothing rather than surface the mistake. A tools entry may be a bare tool
// name (`execution`) or a dotted tool-method reference
// (`execution.read_file`) — the method, when given, must itself be declared
// on that tool, not just the tool.
func validateDeclaredScope(prog *ast.Program, toolRefs, mcpServerNames []string) error {
	for _, ref := range toolRefs {
		if err := validateToolRef(prog, ref); err != nil {
			return err
		}
	}
	for _, name := range mcpServerNames {
		if _, ok := findMCPServer(prog, name); !ok {
			return fmt.Errorf("mcp_server %q is not declared in this program", name)
		}
	}
	return nil
}

func validateToolRef(prog *ast.Program, ref string) error {
	toolName, method, hasMethod := strings.Cut(ref, ".")
	tool, ok := findTool(prog, toolName)
	if !ok {
		return fmt.Errorf("tool %q is not declared in this program", toolName)
	}
	if !hasMethod {
		return nil
	}
	for _, m := range tool.Methods {
		if m.Name == method {
			return nil
		}
	}
	return fmt.Errorf("tool %q has no method %q", toolName, method)
}

// buildScopedPrompt prepends an explicit allowed-tools/mcp_servers block to
// promptText.
func buildScopedPrompt(tools, mcpServers []string, promptText string) string {
	var b strings.Builder
	b.WriteString("[ALLOWED SCOPE]")
	if len(tools) > 0 {
		b.WriteString("\ntools: " + strings.Join(tools, ", "))
	}
	if len(mcpServers) > 0 {
		b.WriteString("\nmcp_servers: " + strings.Join(mcpServers, ", "))
	}
	b.WriteString("\nUse only the tools and mcp_servers listed above for this task.\n\n")
	b.WriteString(promptText)
	return b.String()
}
