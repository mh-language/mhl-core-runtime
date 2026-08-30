# serve

`mhl serve` exposes every pipeline/workflow declared under a directory to
another agent — as MCP tools (`mcp`) or Agent2Agent skills (`a2a`). Both
derive the input contract from the workflow's `input name: Type` declarations
(every input required; unknown inputs rejected), take the tool/skill
description from an optional `description: "..."` body property (a generic
string when absent), and run each call in its own throwaway state directory —
no checkpoint or `context.vars` carry-over.

## `mhl serve mcp <dir>` — MCP tools over stdio

Newline-delimited JSON-RPC 2.0 on stdin/stdout (the form an MCP client uses
when it spawns the server). One tool per declaration; `tools/call` returns the
final variable state (`isError: true` on a run failure). Diagnostics go to
**stderr**; stdout is the raw protocol stream.

Both protocol modes work on one connection: the `initialize`/`initialized`
handshake (revisions `2025-11-25` / `2025-06-18` / `2025-03-26`) and the
stateless `2026-07-28` form — every `tools/*` request then carries
`params._meta` with `io.modelcontextprotocol/protocolVersion` and
`io.modelcontextprotocol/clientCapabilities`, and `server/discover` replaces
`initialize`. A stateless request missing that `_meta` is rejected with
`-32602`.

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Triage","arguments":{"title":"api outage","body":"everything is down"}}}' \
| mhl serve mcp sample/serve
```

## `mhl serve a2a [--addr host:port] <dir>` — A2A skills over HTTP

JSON-RPC 2.0 over HTTP, Agent Card at `/.well-known/agent-card.json`, one skill
per declaration. `message/send` starts a run as a task; the skill and inputs
are named explicitly in the message metadata:

```bash
mhl serve a2a --addr 127.0.0.1:8710 sample/serve &

curl -s localhost:8710/.well-known/agent-card.json

curl -s -X POST localhost:8710/ -d '{"jsonrpc":"2.0","id":1,"method":"message/send",
  "params":{"message":{"role":"user","parts":[],
    "metadata":{"skill":"Triage","input":{"title":"db outage","body":"prod is down"}}}}}'
# -> { "result": { "id": "<task-id>", "status": { "state": "submitted" }, ... } }

curl -s -X POST localhost:8710/ -d '{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"<task-id>"}}'
# -> status.state "completed", artifacts[0].parts[0].text = the run's result JSON
```

`tasks/cancel` with `{"id": "<task-id>"}` cancels an in-flight run.

## Wiring an MCP client

Point any MCP client at the command. For a Claude Desktop-style config:

```json
{
  "mcpServers": {
    "mhl-workflows": {
      "command": "mhl",
      "args": ["serve", "mcp", "/absolute/path/to/sample/serve"]
    }
  }
}
```

- [summarize.mh](summarize.mh) — `Summarize(topic, max_words)`; a `before`-free
  agent step (uses `echo`, so it needs no real LLM)
- [triage.mh](triage.mh) — `Triage(title, body)`; pure string logic, no agent
