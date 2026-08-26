package cli_test

import (
	"strings"
	"testing"
)

// TestAgentBeforeHookFetchesRealDataForPromptInterpolation proves the
// end-to-end wiring for `before: (mcp, tool) -> {...}`: the hook's `mcp`/
// `tool` parameters are maps navigable by declared name
// (`mcp.GitHub.call(...)`, `tool.execution.read_file(...)`), its returned
// object's fields become real "${...}" bindings in the prompt, and `after`
// post-processes the agent's response. Unlike the `tools:`/`mcp_servers:`
// scope text (agent_scope.go), nothing here is a hint the model may ignore
// — mcp/tool are real calls executed by mhl itself before the agent ever
// runs.
func TestAgentBeforeHookFetchesRealDataForPromptInterpolation(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    tools: [execution]
    mcp_servers: [GitHub]

    before: (mcp, tool) -> {
        var mcp_result = mcp.GitHub.call("search", {})
        var tool_result = tool.execution.read_file("x")
        return { mcp_result: mcp_result, tool_result: tool_result }
    }
    after: (mcp, tool, result) -> {
        return result + " [processed]"
    }
}

tool execution {
    read_file(path: string) -> "file-content-for-" + path
}

mcp_server GitHub {
    transport: "stdio"
    command: "sh"
    args: ["-c", "read _; printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"items\":42}}'"]
}
` + wrapStep(`
        var response = Echo.run(prompt: "mcp=${mcp_result} tool=${tool_result}")
        log(response)
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		`mcp={"items":42}`,
		"tool=file-content-for-x",
		"[processed]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

// TestAgentBeforeHookNavigatesMultipleToolsAndMCPServers proves that
// declaring more than one entry in `tools:`/`mcp_servers:` makes every one
// of them reachable by name off the hook's `mcp`/`tool` maps — not just
// the first, which a single-reference design would have left stranded.
func TestAgentBeforeHookNavigatesMultipleToolsAndMCPServers(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    tools: [execution, storage]
    mcp_servers: [GitHub, Jira]

    before: (mcp, tool) -> {
        return {
            a: tool.execution.read_file("x"),
            b: tool.storage.write_json("y"),
            c: mcp.GitHub.call("search", {}),
            d: mcp.Jira.call("search", {})
        }
    }
}

tool execution {
    read_file(path: string) -> "execution:" + path
}

tool storage {
    write_json(path: string) -> "storage:" + path
}

mcp_server GitHub {
    transport: "stdio"
    command: "sh"
    args: ["-c", "read _; printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"github-result\"}'"]
}

mcp_server Jira {
    transport: "stdio"
    command: "sh"
    args: ["-c", "read _; printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"jira-result\"}'"]
}
` + wrapStep(`
        var response = Echo.run(prompt: "${a} / ${b} / ${c} / ${d}")
        log(response)
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"execution:x", "storage:y", "github-result", "jira-result"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %s", want, out)
		}
	}
}

// TestAgentWithoutBeforeAfterIsUnaffected proves an agent declaring neither
// hook behaves exactly as before this feature existed.
func TestAgentWithoutBeforeAfterIsUnaffected(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
}
` + wrapStep(`
        var response = Echo.run(prompt: "hello")
        log(response)
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hello\n") {
		t.Errorf("output missing the plain prompt, got: %s", out)
	}
}

// TestAgentBeforeHookCannotReachUndeclaredTool proves the scoping is real:
// a before hook only receives handles to the agent's own declared
// tools:/mcp_servers:, not any other declaration in the program — calling
// a method through the `tool` map when the agent declared no `tools:` at
// all (so `tool` is nil, not a map with anything in it) fails instead of
// silently reaching some other global declaration.
func TestAgentBeforeHookCannotReachUndeclaredTool(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]

    before: (mcp, tool) -> {
        var x = tool.execution.read_file("x")
        return { mcp_result: "", tool_result: x }
    }
}

tool execution {
    read_file(path: string) -> "should not be reachable"
}
` + wrapStep(`
        var response = Echo.run(prompt: "hi")
    `)

	_, err := run(t, src)
	if err == nil {
		t.Fatal("expected an error navigating the tool map when no tools: were declared, got nil")
	}
}

// TestAgentBeforeHookDottedToolRefRestrictsToThatMethod proves a dotted
// `tools:` entry (`execution.read_file`) narrows the hook's tool reference
// to exactly that method — a call to a different, real method the same
// tool declares (`write_file`) is rejected as out of scope, not silently
// allowed just because the tool itself was reachable.
func TestAgentBeforeHookDottedToolRefRestrictsToThatMethod(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    tools: [execution.read_file]

    before: (mcp, tool) -> {
        var x = tool.execution.write_file("x", "y")
        return { mcp_result: "", tool_result: x }
    }
}

tool execution {
    read_file(path: string) -> "read:" + path
    write_file(path: string, content: string) -> "write:" + path
}
` + wrapStep(`
        var response = Echo.run(prompt: "hi")
    `)

	_, err := run(t, src)
	if err == nil {
		t.Fatal("expected an error calling write_file when only execution.read_file was declared, got nil")
	}
	if !strings.Contains(err.Error(), "write_file") {
		t.Errorf("error should name the out-of-scope method, got: %v", err)
	}
}

// TestAgentBeforeHookDottedToolRefAllowsTheDeclaredMethod is the companion
// clean case: calling exactly the declared method works normally.
func TestAgentBeforeHookDottedToolRefAllowsTheDeclaredMethod(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    tools: [execution.read_file]

    before: (mcp, tool) -> {
        return { mcp_result: "", tool_result: tool.execution.read_file("x") }
    }
}

tool execution {
    read_file(path: string) -> "read:" + path
    write_file(path: string, content: string) -> "write:" + path
}
` + wrapStep(`
        var response = Echo.run(prompt: "${tool_result}")
        log(response)
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "read:x") {
		t.Errorf("output missing the declared method's result, got: %s", out)
	}
}

// TestAgentBeforeHookBareToolRefWinsOverDottedRestriction proves that
// naming a tool both bare and dotted anywhere in `tools:` leaves it fully
// unrestricted — a method-level restriction the same list also grants full
// access to would be meaningless, so the bare form wins regardless of
// declaration order.
func TestAgentBeforeHookBareToolRefWinsOverDottedRestriction(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]
    tools: [execution.read_file, execution]

    before: (mcp, tool) -> {
        return { mcp_result: "", tool_result: tool.execution.write_file("x", "y") }
    }
}

tool execution {
    read_file(path: string) -> "read:" + path
    write_file(path: string, content: string) -> "write:" + path
}
` + wrapStep(`
        var response = Echo.run(prompt: "${tool_result}")
        log(response)
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "write:x") {
		t.Errorf("output missing the now-unrestricted method's result, got: %s", out)
	}
}

// TestAgentAfterHookNoReturnPassesThrough proves an `after` hook that
// doesn't return anything leaves the response unchanged — passthrough is
// the default, not something a hook has to opt into.
func TestAgentAfterHookNoReturnPassesThrough(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]

    after: (mcp, tool, result) -> {
        log("saw response: ${result}")
    }
}
` + wrapStep(`
        var response = Echo.run(prompt: "hello")
        log(response)
    `)

	out, err := run(t, src)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "saw response: hello") {
		t.Errorf("output missing after hook's side effect, got: %s", out)
	}
	if !strings.Contains(out, "hello\n") {
		t.Errorf("output missing the unchanged passthrough response, got: %s", out)
	}
}
