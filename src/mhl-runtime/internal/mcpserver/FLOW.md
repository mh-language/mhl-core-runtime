# `mcpserver` — end-to-end request flow

Three ways to invoke the same workflows. All converge on `execsvc.Run`; what
differs is the transport and **who waits for the run to finish**.

| Flow | Transport | Blocks the client? | Per-step progress? |
|---|---|---|---|
| 1. stdio | one JSON-RPC line on stdin/stdout | yes (one message at a time) | no |
| 2. HTTP synchronous (`tools/call`) | `POST /mcp` | yes, for the whole run | no |
| 3. HTTP asynchronous (`run/*`) | `POST /mcp` | no — returns a `runId` | yes, via `run/status` |

HTTP server config: default bind `127.0.0.1:8711`; `--token` /
`MHL_SERVE_TOKEN` turns on `Authorization: Bearer`; `Origin`, when sent, must
be loopback.

---

## Shared core — `execsvc.Run`

Every `tools/call` / `run/start` ends up here. `Request` carries `Context`
(cancels the run at the next step boundary), `BaseDir` (the run's own
`.mhl/` state tree), `Out` (diagnostics), and — in flow 3 — `OnStep`.

```mermaid
sequenceDiagram
    autonumber
    participant Adap as adapter (callTool / execRun)
    participant X as execsvc.Run
    participant RT as runtime.Runner
    participant IT as interpreter.RunStep

    Adap->>X: Request{Context, Program, Workflow, Inputs, BaseDir, Out, OnStep?}
    X->>X: FindPipeline · ResolveSession · coerce inputs against declared types
    X->>RT: Runner(BaseDir).Session(id).Run(ctx, pipeline, exec, resume)
    loop each step, while ctx not cancelled
        RT->>RT: ctx.Err()? aborts before the step (in-flight step still finishes)
        RT->>X: exec(ctx, step)
        X->>X: writes "step: name" line to Out · OnStep(step, i, total) if set
        X->>IT: RunStep(ctx, prog, step, ...)
        IT-->>X: err | *BreakSignal | *GotoSignal
    end
    RT-->>X: RunResult{Executed, FinalVars}
    X-->>Adap: Result{Vars, Executed, SessionID}  |  error
```

---

## Flow 1 — stdio (`mhl serve mcp <dir>`)

`cli.runServeMCP` without `--http` calls `mcpserver.Serve(ctx, dir,
os.Stdin, os.Stdout, os.Stderr)`. `ctx` is the signal context
(SIGINT/SIGTERM). A single `session` lives for the whole process; messages
are handled in order, one at a time.

```mermaid
sequenceDiagram
    autonumber
    participant C as MCP client
    participant S as Serve (server.go)
    participant D as server.dispatch
    participant X as execsvc.Run

    Note over S: startup — execsvc.Load(dir) builds the workflow map · one session
    C->>S: JSON-RPC line on stdin
    S->>S: bufio.Scanner + json.Unmarshal into rpcMsg
    S->>D: dispatch(ctx, session, msg)
    alt initialize
        D->>D: session.initialized = true · negotiate version
        D-->>S: result (no resultType · serverInfo at top level)
    else notifications (no id)
        D-->>S: nil (nothing to reply)
    else tools/list · tools/call · ping
        D->>D: requireProtocolContext(session) — legacy passes · stateless needs params._meta
        D->>X: callTool → MkdirTemp + execsvc.Run(Context: ctx)
        X-->>D: Result{Vars}  |  runErr
        D-->>S: toolResult(json(vars), isError=runErr!=nil)
    end
    S->>C: JSON-RPC line on stdout (when the reply is not nil)
    Note over S,C: temp dir removed after the call · SIGINT cancels ctx and the run aborts
```

---

## Flow 2 — HTTP synchronous (`POST /mcp` → `tools/call`)

`cli.runServeMCP --http` calls `mcpserver.ServeHTTP(ctx, addr, dir, token,
logw)`. `http.Server.BaseContext` ties `r.Context()` to the server context,
so **client disconnect OR shutdown** cancels the run.

```mermaid
sequenceDiagram
    autonumber
    participant C as HTTP client
    participant H as handleMCP (http.go)
    participant D as server.dispatch
    participant X as execsvc.Run

    C->>H: POST /mcp  { jsonrpc, id, method:"tools/call", params }
    H->>H: bearer token? · Origin loopback? · method POST?
    H->>H: read body (max 8 MiB) · unmarshal into rpcMsg  (error → -32700)
    H->>H: MCP-Protocol-Version header known? (lenient when absent)
    alt Mcp-Session-Id header present
        H->>H: lookup(sid) → 404 if unknown/expired
    else method == "initialize"
        H->>D: dispatch(r.Context(), session, msg)
        H->>H: mint Mcp-Session-Id · store(session) · sweep stale sessions/runs
        H-->>C: 200 + result + Mcp-Session-Id header
    else no session
        H->>H: ephemeral session (stateless path — dispatch requires params._meta)
    end
    H->>D: dispatch(r.Context(), session, msg)
    D->>D: requireProtocolContext(session)
    D->>X: callTool(r.Context()) → MkdirTemp + execsvc.Run
    Note over H,X: the HTTP handler is BLOCKED here for the whole run
    X-->>D: Result{Vars}  |  runErr
    D-->>H: toolResult(...)
    H-->>C: 200 + JSON-RPC envelope   (notification → 202, no body)
    Note over C,H: DELETE /mcp ends the session (204 / 404) · GET → 405
```

---

## Flow 3 — HTTP asynchronous (`run/start` → `run/status` → `run/cancel`)

`run/*` is routed in `handleMCP` **before** `dispatch` (it is HTTP-only — it
needs the `h.runs` registry). It passes the same protocol-context gate as
`tools/*`. The run lives in a goroutine whose context descends from
`h.baseCtx` (the server lifetime), **not** from `r.Context()`.

```mermaid
sequenceDiagram
    autonumber
    participant C as HTTP client
    participant H as handleRun (runs.go)
    participant Reg as h.runs (registry)
    participant G as execRun goroutine
    participant X as execsvc.Run

    C->>H: POST /mcp  run/start { name, arguments }
    H->>H: gate (session or params._meta) · resolve tool  (-32602 if unknown)
    H->>H: ctx,cancel = WithCancel(baseCtx)
    H->>Reg: runs[runId] = asyncRun{ state:"working", cancel, done }
    H->>G: go execRun(ctx, rn, resume=false)
    H-->>C: 200 + { runId, state:"working" }
    Note over H,C: immediate reply — connection released

    Note over G: runs in the background
    G->>X: execsvc.Run(Context: ctx, BaseDir: h.runsDir, Session: runId, OnStep: cb)
    loop each step
        X->>Reg: cb → rn.step / rn.stepIndex / rn.stepTotal / rn.reached / rn.updated  (under rn.mu)
    end
    X-->>G: Result{Skipped, Executed, Vars}  |  runErr
    G->>Reg: rn.state = completed / failed / canceled · rn.resumable = checkpoint on disk · close(rn.done)
    Note over G: completed → remove runsDir/.mhl/state/runId · failed/canceled → keep it for run/resume

    C->>H: POST /mcp  run/status { runId }
    H->>Reg: ownedRun = lookupRun · else reconstructRun (claims it) · reject if owner != caller
    H-->>C: 200 + { state, step, stepIndex, stepTotal, reached[], resumable?, vars?, error? }
    Note over H,C: a run belongs to the session that started it — a non-owner (and an unknown id) both get -32602 "unknown runId"

    opt cancellation
        C->>H: POST /mcp  run/cancel { runId }
        H->>Reg: rn.cancel() and, if "working", rn.state = "canceled"
        H-->>C: 200 + { state:"canceled" }
        Note over G,X: runCtx.Err() at the next step boundary ends the run (in-flight step finishes · execRun does not overwrite "canceled")
    end

    opt resume (workflow declares checkpoint { strategy: "per_step" })
        C->>H: POST /mcp  run/resume { runId, arguments? }
        H->>Reg: lookupRun · else reconstructRun · reject if working or no checkpoint
        H->>Reg: merge arguments over rn.args · fresh ctx,cancel,done · rn.state = "working"
        H->>G: go execRun(ctx, rn, resume=true)
        H-->>C: 200 + { state:"working" }
        Note over G,X: execsvc.Run(Session: runId, Resume: true) → Runner loads the checkpoint,<br/>restores vars, restarts at NextStep (the failed step) · inputs re-applied shadow checkpoint vars
    end

    Note over Reg: run/list returns only the caller's runs · initialize sweeps completed runs older than 1 h (removes the dir) · a resumable run is kept in the registry for the process lifetime so its owner binding holds · on-disk state is GC'd by runtime.PruneExpired · shutdown cancels all and, only for a temp runsDir, deletes it
```

### `asyncRun` states

```
working ──▶ completed        (run finished with no error — state dir removed)
        ├─▶ failed           (runErr != nil and ctx not cancelled — resumable if a checkpoint is on disk)
        └─▶ canceled         (run/cancel, or runErr with ctx cancelled)

failed / canceled ──▶ working   (run/resume, when the workflow has a per_step checkpoint)
```

`reached` is the ordered list of steps that **started** (fed by `OnStep`);
on completion it becomes `Result.Skipped ++ Result.Executed` (authoritative,
and across a resume it includes the steps the resume skipped over). `vars`
appears only in `completed`; `error` only in `failed` / `canceled`;
`resumable` only when `run/resume` can continue the run.

### Ownership

`rn.owner` = `sha256(Mcp-Session-Id)` of the session that ran `run/start`
(hashed so a leaked run view carries no usable session credential). `ownedRun`
gates `run/status` / `run/resume` / `run/cancel`, and `run/list` filters to
the caller's own runs; a non-owner is answered exactly like an unknown
`runId` (`-32602`), so the method is not an existence oracle. Stateless
callers have no session, so `owner` is `""` and all stateless callers share
it — stateless mode has no per-caller isolation. After a restart the owner
session no longer exists; `reconstructRun` then claims the run for the first
caller that names the (128-bit, unguessable) `runId`.

### Persistence

Async run state lives under `<runsDir>/.mhl/state/<runId>/`. `runsDir` is
`--state-dir` / `MHL_SERVE_STATE_DIR` when given (durable — a later process
`run/status`/`run/resume`s a `runId` it never started, via `reconstructRun`),
otherwise a per-process temp dir removed on shutdown. Checkpoints are only
written when the workflow declares `checkpoint { strategy: "per_step" }` — one
small JSON file (`tmp` + `rename`) per completed step, sized by the pipeline's
variable state. `run/status` on a live run stays a pure in-memory read; disk
is touched only on `run/resume` or a status poll after the registry entry is
gone.
