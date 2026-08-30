package cli_test

import (
	"strings"
	"testing"
)

// TestAgentBeforeHookFetchesRealDataForPromptInterpolation proves the
// end-to-end wiring for `before: () -> {...}`: the hook body resolves
// declared `tool`/`extension` names the same way any expression does
// (`GitHub.call(...)`, `execution.read_file(...)`), its returned object's
// fields become real "${...}" bindings in the prompt, and `after`
// post-processes the response (bound as `result`). Nothing here is a hint
// the model may ignore — these are real calls executed by mhl itself before
// the agent ever runs.
func TestAgentBeforeHookFetchesRealDataForPromptInterpolation(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]

    before: () -> {
        var mcp_result = GitHub.call("search", {})
        var tool_result = execution.read_file("x")
        return { mcp_result: mcp_result, tool_result: tool_result }
    }
    after: () -> {
        return result + " [processed]"
    }
}

tool execution {
    read_file(path: string) -> "file-content-for-" + path
}

extension mcp GitHub {
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

// TestAgentBeforeHookFetchesFromMultipleToolsAndExtensions proves a hook body
// can call any number of declared tools and extensions by name.
func TestAgentBeforeHookFetchesFromMultipleToolsAndExtensions(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]

    before: () -> {
        return {
            a: execution.read_file("x"),
            b: storage.write_json("y"),
            c: GitHub.call("search", {}),
            d: Jira.call("search", {})
        }
    }
}

tool execution {
    read_file(path: string) -> "execution:" + path
}

tool storage {
    write_json(path: string) -> "storage:" + path
}

extension mcp GitHub {
    transport: "stdio"
    command: "sh"
    args: ["-c", "read _; printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"github-result\"}'"]
}

extension mcp Jira {
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

// TestAgentHookWithParametersIsRejected proves the hook signature is fixed at
// zero parameters — a leftover `(mcp, tools) -> ...` gets a pointed error.
func TestAgentHookWithParametersIsRejected(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]

    before: (mcp, tools) -> {
        return {}
    }
}
` + wrapStep(`
        var response = Echo.run(prompt: "hi")
    `)

	_, err := run(t, src)
	if err == nil || !strings.Contains(err.Error(), "takes no parameters") {
		t.Fatalf("expected a no-parameters error, got: %v", err)
	}
}

// TestAgentAfterHookNoReturnPassesThrough proves an `after` hook that doesn't
// return a string leaves the response unchanged — passthrough is the default,
// not something a hook has to opt into — and that `result` is bound in the
// body.
func TestAgentAfterHookNoReturnPassesThrough(t *testing.T) {
	src := `
agent Echo {
    command: "echo"
    args: ["${prompt}"]

    after: () -> {
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
