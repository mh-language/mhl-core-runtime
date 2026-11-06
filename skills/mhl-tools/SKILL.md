---
name: mhl-tools
description: Give mhl programs side effects and integrations. Use for tool declarations, native ops (cmd, fs, git, http, json, time), memory stores, extension mcp and extension a2a clients, and credentials through env().
---

# mhl tools and integrations

Assumes the `mhl-language` skill. Exact signatures are in the `mhl-language` skill's
`references/stdlib.md`.

## The failure convention

Native ops fail in two different ways. Choose your handling per op:

| Op | On failure | You must |
|---|---|---|
| `cmd.exec`, `git.status`, `git.add`, `git.commit` | returns `{stdout, stderr, exit_code}` | check `exit_code` |
| `http.*` non-2xx | returns `{status, ok, body, ...}` | check `.ok`, or set `raise_for_status: true` |
| `http.*` transport error, `fs.read`, `json.parse`, `git.diff`, `git.log`, `git.rev_parse` | raises | `try` only if you can recover |

An unchecked `cmd.exec` is the classic silent bug: the step passes while the command
failed. Wrap it once and reuse the wrapper.

## Declaring tools

A `tool` is a namespace of methods that gives steps, hooks and tests one reusable,
testable API over native ops. Put logic here and keep steps thin.

```mh
tool Shell {
    // Fails the step on a non-zero exit, so callers need no check.
    must(argv: string[]): string {
        var result = cmd.exec(argv, timeout: 30s)
        if (result.exit_code != 0) fail(self.describe_failure(argv, result.stderr))
        return result.stdout.trim()
    }

    // internal: callable through self. and from this file's tests only.
    internal describe_failure(argv: string[], stderr: string): string ->
        "${argv.join(" ")} failed: ${stderr.trim()}"
}

test shell {
    describe returns_trimmed_stdout {
        are_equal(Shell.must(["echo", "hi"]), "hi")
    }

    describe nonzero_exit_fails {
        var message = ""
        try {
            Shell.must(["sh", "-c", "echo nope >&2; exit 3"])
        } catch (e) {
            message = "${e}"
        }
        is_true(message.contains("nope"))
    }

    describe internal_helpers_are_testable_here {
        are_equal(Shell.describe_failure(["ls"], "x\n"), "ls failed: x")
    }
}
```

- Annotate parameters and returns. Calls are checked by lint where it can see the types.
- Bind arguments by position, in declaration order. Names in a tool call are ignored.
- Use an expression body (`name(x): T -> expr`) or a block (`name(x): T { ... }`).
- Keep control-flow exits (`goto`, `pause`, `complete`, `break`) in steps. `fail()` belongs
  in tools freely.
- Share a value across methods with `const` or `var` in the tool body, and compose with
  `self.other(...)`. Write durations as literals at the use site.

## Processes, files, HTTP

- Pass argv arrays: `cmd.exec(["git", "log", "-1"])`. Each element stays one argument, so
  caller input cannot inject shell syntax. Use a string only when you need a pipe or
  redirect, and never build it from caller input.
- Set `timeout:` on every `cmd` and `http` call.
- `fs.write` truncates, `fs.append` creates, and `dir.clear` empties recursively. Relative
  paths resolve against the process working directory, not the `.mh` file.
- Strings are UTF-8 inside mhl. Read legacy files with `encoding:`, for example
  `fs.read("a.csv", encoding: "windows-1252")`.
- Use `git.status(dir: path)` to target another checkout; every `git.*` op takes `dir`.
- Datetimes are plain RFC3339 strings, handled through `time.*`.
- `os.*` describes the host running mhl. Under `mhl serve` that is the server, so keep it
  out of prompts and treat it as never identifying the caller.

```mh
tool Api {
    get_job(id: string): any {
        var response = http.get(
            url: "https://api.example.com/jobs/${id}",
            auth: {bearer: env("API_TOKEN")},
            timeout: 10s
        )
        if (!response.ok) fail("job ${id}: HTTP ${response.status}")
        return response.json
    }
}
```

## Choosing where state lives

| Need | Use |
|---|---|
| Passed between steps of one run | pipeline `var` |
| Read by `stop_when`, or carried across loop iterations and runs | `mem x = init` |
| Named store with keys, or a file another tool reads | `memory` declaration |
| Audit trail | `memory` with `type: "append_log"` or `"jsonl"` |

```mh
memory Scratch {
    type: "kv"
    store: "memory"       // one execution only
}

memory Session {
    type: "json"
    path: ".mhl/session.${context.session_id}.json"
    path_guard: "no_traversal"
}

memory Audit {
    type: "jsonl"
    path: ".mhl/audit.jsonl"
}

pipeline Example {
    step Use {
        Session.set("config", {retry: 3})
        log(Session.get("config::retry"))    // :: navigates nested keys
        Session.remove("config::retry")
        log(Session.get("missing", "default"))
        Audit.append({event: "done"})
    }
}

test scratch {
    describe kv_reads_back_with_a_default {
        Scratch.set("attempt", 1)
        are_equal(Scratch.get("attempt"), 1)
        are_equal(Scratch.get("other", 0), 0)
    }
}
```

| `type` | Methods | Persists |
|---|---|---|
| `kv` (`store: "memory"`) | `set`, `get` | no |
| `json` (`path:`) | `set`, `get`, `remove` | yes |
| `append_log` (`path:`) | `append` (text line) | yes |
| `jsonl` (`path:`) | `append` (any value, stamped `{value, ts}`) | yes |

Scope a `path:` with `${context.session_id}` so concurrent runs never share a file. Any
caller-controlled value in a path needs `path_guard: "no_traversal"`; interpolation does
not sanitize `..` on its own.

## Calling MCP servers and A2A agents

An `extension` declares a use of an external capability. `mcp` and `a2a` are built in;
other kinds come from installed adapters (`extension data Cache from "mhl.data.redis" {...}`).

```mh
extension mcp Issues {
    transport: "stdio"
    command: "sh"            // a real server: command "npx", args [...]
    args: ["-c", "read _; printf '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"issue-42\"}'"]
}

agent Triager {
    command: "echo"
    args: ["${prompt}"]

    // The model cannot call MCP itself, so the hook does it first.
    before: () -> {
        return { issue: Issues.call("get_issue", {number: 42}) }
    }
}

test triager {
    describe mcp_result_reaches_the_prompt {
        is_true(Triager.run(prompt: "Triage ${issue}").contains("issue-42"))
    }
}
```

```mh-fragment
extension mcp Tracker {
    transport: "http"
    url: "https://mcp.example.com"
    headers: { "Authorization": "Bearer " + env("MCP_TOKEN") }
}

extension a2a Translator {
    url: "https://translator.example.com/a2a"
    headers: { "Authorization": "Bearer " + env("A2A_TOKEN") }
    poll_timeout: 120s
}

// In a step:
var issues = Tracker.call("search_issues", {query: "is:open"})
var catalog = Tracker.list_tools()
var reply = Translator.send("Translate 'hello' to French")
log(reply.text, reply.context_id)
```

Each call is stateless. `.call` on MCP takes the tool name and an arguments object. A
server asking for interactive input (`input_required`) is rejected, so design around
one-shot calls.

## Credentials

`env("KEY")` behaves differently by position:

| Position | Unset value |
|---|---|
| Agent `command`/`args`, extension properties | fails closed, redacted from logs |
| Anywhere in a step or tool | returns `""` silently |
| `env("KEY", "default")`, anywhere | returns the default, no fail-closed, no redaction |

So in a step, guard the value yourself before using it:
`if (env("API_TOKEN").is_empty()) fail("API_TOKEN is required")`. Keep secrets in the
environment, out of `.mh` source, `args:`, `stdin:` and prompts, and treat `.mh` files,
logs and `.mhl/` state as code to review before sharing.
