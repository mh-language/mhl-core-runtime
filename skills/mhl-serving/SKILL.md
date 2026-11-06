---
name: mhl-serving
description: Expose mhl workflows to other agents with mhl serve. Use for MCP tools, A2A skills, the mhl_run_* async tools, input and output contracts for callers, human-in-the-loop resume, --token and --state-dir hardening, and mhl pack.
---

# mhl serving

Assumes the `mhl-language` and `mhl-workflows` skills. `mhl serve` turns a directory of
`.mh` files into a server with no server code: each `pipeline`, `workflow` or alias becomes
one MCP tool or A2A skill, published by the file that declares it. This skill is the
server side. Calling someone else's server is `extension mcp`, covered in `mhl-tools`.

## Design the workflow as an API

The declaration **is** the tool contract, so write it for the calling model:

```mh
enum Severity { low, high }

type TriageInput = { title: string, body: string, severity?: Severity }
type TriageOutput = { priority: string, reason: string }

tool Rules {
    priority(title: string): string -> if (title.contains("outage")) "urgent" else "normal"
}

// The description is all a calling model sees, so state what the tool does.
workflow TriageIssue(req: TriageInput): TriageOutput {
    description: "Classifies an incoming issue and explains the priority."

    var priority = "normal"

    step Classify {
        self.priority = Rules.priority(req.title)
    }

    output: {
        priority: priority
        reason: "matched on title: ${req.title}"
    }
}

test triage_issue {
    describe outage_is_urgent {
        var result = TriageIssue.run(inputs: {title: "api outage", body: "down"})
        are_equal(result.output.priority, "urgent")
    }
}
```

| Declared | Becomes |
|---|---|
| `description: "..."` | tool description; otherwise a generic `Runs the mhl workflow "X".` |
| typed signature or `input x: T` | `inputSchema`, unknown inputs rejected |
| `x?:` field, or `input x: T = default` | optional in `inputSchema` |
| `enum` type | `"enum": [...]` list, so callers see legal values |
| `: Out` result type | `outputSchema`; a successful result is `structuredContent` checked against it |
| `output:` | exactly the keys returned; without it, every var is returned |

Then shape the surface:

- Mark helpers `internal workflow`. Publish only what callers should invoke.
- Use aliases to publish one flow as several narrow tools:
  `workflow Delivery = ArtifactFlow with { level: Level.delivery } { description: "..." }`.
- Always declare `output:` on a served workflow. It keeps intermediate state, prompts and
  tokens out of responses.
- A workflow imported by several files is published once, by its declaring file.

## Transports

| Command | Use |
|---|---|
| `mhl serve mcp <dir>` | stdio, when the client spawns the server |
| `mhl serve mcp --http [--addr h:p] <dir>` | Streamable HTTP at `/mcp`, default `127.0.0.1:8711`; also `/healthz`, `/readyz`, `/metrics` |
| `mhl serve a2a [--addr h:p] <dir>` | A2A at `/.well-known/agent-card.json`, default `127.0.0.1:8710` |

Smoke-test a directory without any client:

```sh
mhl lint ./workflows
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"TriageIssue","arguments":{"title":"api outage","body":"down"}}}' \
  | mhl serve mcp ./workflows
```

Client configuration, with an absolute path:

```json
{ "mcpServers": { "workflows": { "command": "mhl", "args": ["serve", "mcp", "/abs/path/workflows"] } } }
```

## Sync, async, and human approval

| Call | Behavior | Fits |
|---|---|---|
| `tools/call <Workflow>` | blocks until done, throwaway state | short workflows |
| `mhl_run_start` then `mhl_run_status` | returns a `runId` at once; poll for step and state | long workflows, anything that pauses |

The `mhl_run_*` tools (`start`, `status`, `resume`, `cancel`, `list`, `logs`) exist on the
HTTP transport only, and any MCP client can call them as ordinary tools.

For human approval, write a `pause()` gate (see `mhl-workflows`), start it with
`mhl_run_start`, and continue with `mhl_run_resume {runId, arguments: {approved: true}}`.
The arguments merge over the originals and the gate step re-enters.

Gated workflows must go through the async path. A synchronous `tools/call` that hits
`pause()` answers `isError: false` with the partial variable state, skipping `output:`,
and offers no way to continue: the caller reads a success that never happened.

Arguments to `mhl_run_start` and `mhl_run_resume` are capped at 256 KiB and persisted with
the run, so pass large payloads by reference.

Read-only introspection lives in resources: `mhl://workflow/<name>` is a manifest of steps,
inputs and dependencies. The `.mh` source is never served.

## Hardening

Before binding beyond loopback:

- Set `--token` (or `MHL_SERVE_TOKEN`). It is one shared secret, so run one trust domain
  per server.
- Behind an authenticating gateway, add `--principal-header` so runs are owned per caller.
  `context.principal` then records who started a run. Use it for audit, not authorization.
- Set `--state-dir` (or `MHL_SERVE_STATE_DIR`) so paused runs survive a restart, and keep
  it readable by the server user only. Without it, run state is lost on restart.
- Set `--max-concurrent-runs` and `--drain-timeout`.
- Every served workflow runs with the server's credentials and filesystem. Treat tool
  inputs as untrusted: argv arrays, `path_guard: "no_traversal"`, and no `os.*` in prompts.

For several replicas, use one `extension state` with the `cas` capability. A store without
it needs `--single-replica`.

## Sealed deployments

To ship workflows without their source on disk, seal them and serve from memory:

```sh
mhl pack ./workflows -o workflows.dmpk --new-key      # prints the key on stdout, once
MHL_SERVE_WORKFLOWS_KEY=<key> mhl serve mcp --http --workflows-pack workflows.dmpk .
```

The key is accepted by environment only. Every `.mh`, `prompt from`, `skill from` and
`schema from` file is read from the pack. Files a subprocess opens by path, such as a
schema passed through `.path`, must still exist on the host.

## Before publishing

- `mhl lint <dir>` and `mhl test <dir>` pass.
- `tools/list` shows only intended tools, each with a real description.
- Every served workflow declares `output:`, and no pausing workflow is reachable as a
  plain synchronous call by a client that would misread it.
- Each workflow's steps are safe to repeat: a client retry is a second run.
- Long or gated workflows are documented to use `mhl_run_start`.
