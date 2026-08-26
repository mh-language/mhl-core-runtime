package cli_test

import (
	"strings"
	"testing"
)

// TestAgentDeclaredToolsFoldIntoEveryRun proves an agent's own
// `tools:`/`mcp_servers:` properties (previously parsed and silently
// ignored) shape every `.run()` call's prompt with an explicit
// allowed-scope instruction.
func TestAgentDeclaredToolsFoldIntoEveryRun(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    tools: [execution]
    mcp_servers: [Repository]
}

tool execution {
    read_file(path: string) -> fs.read(path)
}

mcp_server Repository {
    transport: "stdio"
    command: "true"
}
` + wrapStep(`
        var response = Echo.run(prompt: "hello")
        log(response)
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"[ALLOWED SCOPE]", "tools: execution", "mcp_servers: Repository", "hello"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

// TestAgentWithoutToolsPropertyIsUnaffected proves an agent declaring
// neither `tools:` nor `mcp_servers:` behaves exactly as before this
// feature existed: the prompt reaches the agent unchanged.
func TestAgentWithoutToolsPropertyIsUnaffected(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
}
` + wrapStep(`
        var response = Echo.run(prompt: "hello world")
        log(response)
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hello world\n") {
		t.Errorf("output missing the unscoped prompt echoed back verbatim, got: %s", out)
	}
	if strings.Contains(out, "ALLOWED SCOPE") {
		t.Errorf("output should not contain a scope block when the agent declares no tools/mcp_servers, got: %s", out)
	}
}

// TestAgentDeclaredToolsAcceptDottedToolMethodReferences proves
// validateDeclaredScope handles the dotted `tool.method` reference shape
// (e.g. `tools: [execution.read_file]`), not just a bare tool name.
func TestAgentDeclaredToolsAcceptDottedToolMethodReferences(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    tools: [execution.read_file, execution.write_file]
}

tool execution {
    read_file(path: string) -> fs.read(path)
    write_file(path: string, content: string) -> fs.write(path, content)
}
` + wrapStep(`
        var response = Echo.run(prompt: "hello")
        log(response)
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "tools: execution.read_file, execution.write_file") {
		t.Errorf("output missing dotted tool references, got: %s", out)
	}
}

// TestAgentDeclaredToolsRejectUndeclaredMethod proves a dotted reference to
// a method the tool doesn't actually have is a typo caught at run time, not
// silently accepted.
func TestAgentDeclaredToolsRejectUndeclaredMethod(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    tools: [execution.delete_everything]
}

tool execution {
    read_file(path: string) -> fs.read(path)
}
` + wrapStep(`
        var response = Echo.run(prompt: "hello")
    `)

	_, err := run(t, src)
	if err == nil {
		t.Fatal("expected an error for a tool reference naming an undeclared method, got nil")
	}
	if !strings.Contains(err.Error(), "delete_everything") {
		t.Errorf("error should name the undeclared method, got: %v", err)
	}
}

// TestAgentDeclaredToolsRejectUndeclaredTool proves a typo'd tool name is
// caught rather than silently narrowing the scope to nothing.
func TestAgentDeclaredToolsRejectUndeclaredTool(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    tools: [does_not_exist]
}
` + wrapStep(`
        var response = Echo.run(prompt: "hello")
    `)

	_, err := run(t, src)
	if err == nil {
		t.Fatal("expected an error for an undeclared tool, got nil")
	}
	if !strings.Contains(err.Error(), "does_not_exist") {
		t.Errorf("error should name the undeclared tool, got: %v", err)
	}
}

// TestAgentDeclaredMCPServerRejectsUndeclaredServer proves a typo'd
// mcp_server name is caught the same way.
func TestAgentDeclaredMCPServerRejectsUndeclaredServer(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    mcp_servers: [DoesNotExist]
}
` + wrapStep(`
        var response = Echo.run(prompt: "hello")
    `)

	_, err := run(t, src)
	if err == nil {
		t.Fatal("expected an error for an undeclared mcp_server, got nil")
	}
	if !strings.Contains(err.Error(), "DoesNotExist") {
		t.Errorf("error should name the undeclared mcp_server, got: %v", err)
	}
}
