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

## `mhl serve mcp --http [--addr host:port] [--token t] [--state-dir path] <dir>` — MCP over Streamable HTTP

The same MCP tools over the transport a **networked** client uses: one
JSON-RPC message per `POST /mcp`, JSON responses only (no SSE). Default bind
`127.0.0.1:8711`. `--token` (or `MHL_SERVE_TOKEN`) turns on
`Authorization: Bearer` enforcement; the `Origin` header, when present, must
be loopback.

Both protocol modes work here too. Standard lifecycle — `initialize` returns
an `Mcp-Session-Id` header the client echoes on every later request:

```bash
mhl serve mcp --http --addr 127.0.0.1:8711 sample/serve &

sid=$(curl -sD - -o /dev/null -X POST localhost:8711/mcp -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}' \
  | awk 'tolower($1)=="mcp-session-id:"{print $2}' | tr -d '\r')

curl -s -X POST localhost:8711/mcp -H 'content-type: application/json' -H "Mcp-Session-Id: $sid" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'

curl -s -X POST localhost:8711/mcp -H 'content-type: application/json' -H "Mcp-Session-Id: $sid" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Triage","arguments":{"title":"api outage","body":"everything is down"}}}'

curl -s -X DELETE localhost:8711/mcp -H "Mcp-Session-Id: $sid"   # 204 — end the session
```

Or stateless — no session, every request carries `params._meta` (an unknown
`Mcp-Session-Id` is `404`; missing `_meta` and no session is `-32602`):

```bash
curl -s -X POST localhost:8711/mcp -H 'content-type: application/json' -d '{"jsonrpc":"2.0","id":1,
  "method":"tools/call","params":{"name":"Triage","arguments":{"title":"x","body":"y"},
    "_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",
             "io.modelcontextprotocol/clientCapabilities":{}}}}'
```

A real client points at the URL directly, e.g.
`claude mcp add --transport http mhl-local http://127.0.0.1:8711/mcp`.

### Async: start now, poll for the step later

`tools/call` blocks until the workflow finishes. For a long one, `run/start`
returns a `runId` right away and you poll `run/status` for the current step,
the steps reached so far, and the final `vars`. `run/cancel` stops it.
(Gated by the same protocol context as `tools/*` — a session, as here, or
`params._meta`.)

`Report` (see [report.mh](report.mh)) is a four-step, ~4s pipeline built to
show this:

```bash
rid=$(curl -s -X POST localhost:8711/mcp -H 'content-type: application/json' -H "Mcp-Session-Id: $sid" \
  -d '{"jsonrpc":"2.0","id":4,"method":"run/start","params":{"name":"Report","arguments":{"topic":"caching"}}}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["result"]["runId"])')

# poll a few times while it runs
curl -s -X POST localhost:8711/mcp -H 'content-type: application/json' -H "Mcp-Session-Id: $sid" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":5,\"method\":\"run/status\",\"params\":{\"runId\":\"$rid\"}}"
# -> {"state":"working","step":"Gather","stepIndex":1,"stepTotal":4,"reached":["Gather"], ...}
# -> {"state":"working","step":"Summarize","stepIndex":3,"stepTotal":4,"reached":["Gather","Analyze","Summarize"], ...}
# -> {"state":"completed","reached":["Gather","Analyze","Summarize","Publish"],"vars":{"published":"published: ...", ...}}

# stop one mid-flight
curl -s -X POST localhost:8711/mcp -H 'content-type: application/json' -H "Mcp-Session-Id: $sid" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":6,\"method\":\"run/cancel\",\"params\":{\"runId\":\"$rid\"}}"
# -> {"state":"canceled", ...}
```

`run/list` returns the caller's own runs. Each run is **owned by the session
(`Mcp-Session-Id`) that started it** — `run/status`, `run/resume`,
`run/cancel` and `run/list` only act for that session; another caller sees
`unknown runId`. Stateless callers have no session and share one anonymous
owner. Terminal runs stay pollable for an hour, then are swept; a server
shutdown cancels any still-running.

### Human-in-the-loop: `run/resume`

A workflow that declares `checkpoint { strategy: "per_step" }` and stops at a
**failing step** keeps its checkpoint on disk. `run/resume {runId,
arguments?}` continues it from that step; `arguments` are merged over the
originals — that is where an approval decision goes. `Approval` (see
[approval.mh](approval.mh)) gates on `approved == "yes"`:

```bash
rid=$(curl -s -X POST localhost:8711/mcp -H 'content-type: application/json' -H "Mcp-Session-Id: $sid" \
  -d '{"jsonrpc":"2.0","id":7,"method":"run/start","params":{"name":"Approval","arguments":{"request":"ship it","approved":"no"}}}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["result"]["runId"])')

curl -s -X POST localhost:8711/mcp -H 'content-type: application/json' -H "Mcp-Session-Id: $sid" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":8,\"method\":\"run/status\",\"params\":{\"runId\":\"$rid\"}}"
# -> {"state":"failed","step":"Gate","reached":["Prepare","Gate"],"resumable":true,"error":"...awaiting approval..."}

# a human approves
curl -s -X POST localhost:8711/mcp -H 'content-type: application/json' -H "Mcp-Session-Id: $sid" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"run/resume\",\"params\":{\"runId\":\"$rid\",\"arguments\":{\"approved\":\"yes\"}}}"
# -> {"state":"working", ...}  then run/status -> {"state":"completed","vars":{...}}
```

Start the server with `--state-dir <dir>` (or `MHL_SERVE_STATE_DIR`) to make
run state **survive a restart**: a later process pointed at the same
directory can `run/status` and `run/resume` a `runId` it never started.
Without it, run state is per-process and lost on restart.

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
- [report.mh](report.mh) — `Report(topic)`; four steps, ~1s each, for
  watching `run/status` advance step by step (and testing `run/cancel`)
- [approval.mh](approval.mh) — `Approval(request, approved)`; a `per_step`
  checkpoint plus a `fail()` gate — the `run/start` → `run/resume` HITL loop
