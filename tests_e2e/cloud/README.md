# cloud

Test fixtures for running `mhl serve mcp --http` the way it is deployed in the
EKS / multi-team scenario: one pod behind an authenticated API Gateway, several
teams driving a long, human-gated workflow over the async `run/*` surface.

Two workflows:

- [docs-workflow.mh](docs-workflow.mh) — `DocPipeline(repo, approved)`: the
  daily documentation workflow. `per_step` checkpoint + a `fail()` review gate —
  the `run/start` → `run/status` → `run/resume` loop.
- [slow-build.mh](slow-build.mh) — `SlowBuild(target)`: ~9s, `checkpoint {
  enabled: true }` with **no** `strategy` — for drain, shutdown-checkpoint and
  concurrency tests. Its `Ship` step declares `timeout 1s` over a 3s sleep, so
  it self-terminates (`failed`, then `run/resume` re-enters `Ship`); raise it
  to `timeout 10s` to let the run complete.

## What this exercises

| Capability | Flag / endpoint | Status |
|---|---|---|
| Liveness / readiness probes | `GET /healthz`, `GET /readyz` | available |
| Bearer auth (gateway↔mhl shared secret) | `--token` / `MHL_SERVE_TOKEN` | available |
| Per-principal run isolation | `--principal-header` / `MHL_SERVE_PRINCIPAL_HEADER` (+ `context.principal`) | available |
| Async execution, HITL resume | `run/start` `run/status` `run/resume` `run/cancel` `run/list` | available |
| Run state survives a pod restart | `--state-dir` / `MHL_SERVE_STATE_DIR` | available |
| Graceful drain on SIGTERM | `--drain-timeout` / `MHL_SERVE_DRAIN_TIMEOUT` | available |
| Shutdown checkpoint without `per_step` | (automatic when `checkpoint { enabled: true }`) | available |
| Bounded concurrency + `queued` state | `--max-concurrent-runs` / `MHL_SERVE_MAX_CONCURRENT_RUNS` | available |
| Per-run log retrieval | `run/logs { runId, since? }` | available |
| Prometheus metrics | `GET /metrics` | available |
| Structured JSON lifecycle logs | stderr (`log/slog`, keyed by `runId` + `owner`) | available |
| Durable state in an external store | `extension store <Name> { dir: ... }` in the dir | available (single-replica) |
| Per-step wall-clock cap | `step <Name> timeout <dur> { ... }` (also on a `parallel` group) | available |

**Multi-replica:** an `extension store` declaration (Phase 3a) puts run
checkpoints + sessions in a shared backend, so any pod can `run/status` /
`run/resume` a run from disk. The *live* run registry (the goroutine that
cancels a run, streams its logs) is still per-pod — but a step's `timeout`
clause means a runaway or orphaned run self-terminates on whatever pod is
running it, and the terminal state is then visible everywhere from the shared
store, so cross-pod *live* coordination is deferred (Phase 5, optional).
Recommended: one replica plus the shared store for lossless restarts, and a
`timeout` on any step that calls a model or a slow command.

### Durable state: `--state-dir` vs a `store` extension

- **`--state-dir /state`** — checkpoints as JSON files on that path (a PVC in
  K8s). Simple; one pod.
- **`extension store S { dir: ... }`** in a `.mh` file under the serve dir —
  binds a `store`-kind extension (`tests/extensions/store-fs` is the reference;
  DynamoDB/Redis backends are separate binaries). All `run/*` and `session/*`
  keys go there. `--state-dir` then only holds the interpreter's scratch files.

## Build

```bash
# from the mhl repo
make -C src/mhl-runtime build      # -> src/mhl-runtime/dist/mhl
export MHL=$PWD/src/mhl-runtime/dist/mhl
```

Or install `mhl` onto `PATH` and drop the `$MHL` prefix below.

## Run it like a pod

```bash
STATE=$(mktemp -d)

"$MHL" serve mcp --http \
  --addr 0.0.0.0:8711 \
  --token "$MHL_TEST_TOKEN" \
  --principal-header X-Mhl-Principal \
  --state-dir "$STATE" \
  --drain-timeout 30s \
  --max-concurrent-runs 4 \
  tests/cloud &
SERVER=$!
```

- **`--addr 0.0.0.0:8711`** — bind all interfaces (a container needs this). With
  a non-loopback bind and no `--token`, the server prints an
  "endpoint is unauthenticated" warning to stderr.
- **`--token`** — the **gateway↔mhl shared secret**: every `POST /mcp` must
  carry `Authorization: Bearer <token>`. It is *not* the end-user credential —
  the API Gateway authorizer validates the real JWT and forwards this secret.
  `/healthz`, `/readyz`, `/metrics` are **not** authenticated — a kubelet probe
  carries no token.
- **`--principal-header X-Mhl-Principal`** — the header the gateway authorizer
  puts the verified caller identity in (from the JWT `sub` / `cognito:groups` /
  etc.). mhl keys run ownership on it, so `run/list` / `run/status` /
  `run/resume` / `run/cancel` / `run/logs` isolate per principal, and a run
  reclaimed after a restart is bound to its original principal. Requires
  `--token` (without the shared secret the header is client-spoofable, so mhl
  refuses to start). The value also reaches a workflow as read-only
  `context.principal`.
- **`--state-dir`** — run checkpoints live here; a restarted process pointed at
  the same directory can `run/status` / `run/resume` a `runId` it never started.
- **`--drain-timeout 30s`** — on SIGTERM, stop accepting new work and give
  in-flight async runs up to 30s to finish before cancelling them. `0` (the
  default) cancels immediately.

Everything below assumes:

```bash
BASE=http://127.0.0.1:8711
AUTH="Authorization: Bearer $MHL_TEST_TOKEN"
JSON="content-type: application/json"
```

§3–§7 use `-H "$AUTH"` only; add `-H "X-Mhl-Principal: you@acme.com"` to key
those runs on a principal instead of the session.

---

## 1. Probes

```bash
curl -s -o /dev/null -w '%{http_code}\n' $BASE/healthz    # 200
curl -s -o /dev/null -w '%{http_code}\n' $BASE/readyz     # 200 (ready) / 503 (draining)
```

Kubernetes: `/healthz` is the liveness probe, `/readyz` the readiness probe.
`/readyz` flips to 503 the instant a shutdown begins, so the Service stops
routing new requests here while the drain runs; `/healthz` stays 200 (the pod is
alive and must not be restarted out from under its draining runs).

## 2. Auth &amp; per-principal isolation

```bash
# no token -> 401
curl -s -o /dev/null -w '%{http_code}\n' -X POST $BASE/mcp -H "$JSON" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'

# init two sessions with different principals (a gateway would set the header)
initas() {
  curl -sD - -o /dev/null -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "X-Mhl-Principal: $1" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}' \
    | awk 'tolower($1)=="mcp-session-id:"{print $2}' | tr -d '\r'
}
sidA=$(initas alice@acme.com); sidB=$(initas bob@acme.com)

# alice starts a run
rid=$(curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "X-Mhl-Principal: alice@acme.com" -H "Mcp-Session-Id: $sidA" -d '{
  "jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"api","approved":"no"}}}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["result"]["runId"])')

# bob's run/list is empty; probing alice's runId is "unknown"
curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "X-Mhl-Principal: bob@acme.com" -H "Mcp-Session-Id: $sidB" \
  -d '{"jsonrpc":"2.0","id":3,"method":"run/list"}'                                   # -> {"runs":[]}
curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "X-Mhl-Principal: bob@acme.com" -H "Mcp-Session-Id: $sidB" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"run/status\",\"params\":{\"runId\":\"$rid\"}}"  # -> -32602 unknown runId
```

- `--token` alone → 401 without the bearer; it is the gateway↔mhl secret, not
  the user's credential.
- `X-Mhl-Principal` (verified upstream) is what isolates callers: `run/*` only
  act for a matching principal, a non-owner gets the same `-32602` as a
  nonexistent `runId`, and after a restart the persisted owner means only the
  original principal can reclaim a run (§5).
- Without `--principal-header` the fallback is per-session isolation (one
  `Mcp-Session-Id` = one owner), unchanged from before.

## 3. The daily documentation workflow (async + HITL)

`DocPipeline` is minutes-long in reality and gated on a human — too long for a
blocking `tools/call` behind API Gateway's ~29s integration timeout. Use
`run/start` and poll.

```bash
rid=$(curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "Mcp-Session-Id: $sid" -d '{
  "jsonrpc":"2.0","id":3,"method":"run/start",
  "params":{"name":"DocPipeline","arguments":{"repo":"payments-api","approved":"no"}}}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["result"]["runId"])')
echo "runId: $rid"

# poll — advances Draft -> Review, then parks
curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "Mcp-Session-Id: $sid" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"run/status\",\"params\":{\"runId\":\"$rid\"}}"
# -> {"state":"failed","step":"Review","reached":["Draft","Review"],"resumable":true,
#     "error":"...awaiting documentation review for payments-api..."}

# a reviewer approves -> arguments are merged over the originals
curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "Mcp-Session-Id: $sid" -d "{
  \"jsonrpc\":\"2.0\",\"id\":5,\"method\":\"run/resume\",
  \"params\":{\"runId\":\"$rid\",\"arguments\":{\"approved\":\"yes\"}}}"
# -> {"state":"working", ...}

curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "Mcp-Session-Id: $sid" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":6,\"method\":\"run/status\",\"params\":{\"runId\":\"$rid\"}}"
# -> {"state":"completed","reached":["Draft","Review","Publish"],
#     "vars":{"published":"published docs for payments-api (reviewed)", ...}}
```

`run/list` returns this session's runs. `run/cancel {"runId": ...}` stops one.

## 4. Graceful drain

```bash
# start a genuinely-working run
rid=$(curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "Mcp-Session-Id: $sid" -d '{
  "jsonrpc":"2.0","id":7,"method":"run/start",
  "params":{"name":"SlowBuild","arguments":{"target":"web"}}}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["result"]["runId"])')

sleep 1
kill -TERM $SERVER          # SIGTERM, like a pod eviction

# during the drain window:
curl -s -o /dev/null -w '%{http_code}\n' $BASE/readyz      # 503
curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "Mcp-Session-Id: $sid" -d '{
  "jsonrpc":"2.0","id":8,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"x"}}}'
# -> {"error":{"code":-32000,"message":"server is draining — not accepting new runs"}}

wait $SERVER               # returns only after SlowBuild finishes (or 30s elapses)
```

With `--drain-timeout 0` (the default) the run is cancelled the moment SIGTERM
arrives and the process exits within ~5s. Set the pod's
`terminationGracePeriodSeconds` a little **above** `--drain-timeout`.

## 5. Run state survives a restart

```bash
STATE=$(mktemp -d)
"$MHL" serve mcp --http --addr 127.0.0.1:8711 --token "$MHL_TEST_TOKEN" --state-dir "$STATE" tests/cloud &

# start DocPipeline with approved=no so it parks at the Review gate
rid=$(curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -d '{
  "jsonrpc":"2.0","id":1,"method":"run/start",
  "params":{"name":"DocPipeline","arguments":{"repo":"billing","approved":"no"},
    "_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",
             "io.modelcontextprotocol/clientCapabilities":{}}}}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["result"]["runId"])')

kill %1; wait 2>/dev/null                    # kill the server
"$MHL" serve mcp --http --addr 127.0.0.1:8711 --token "$MHL_TEST_TOKEN" --state-dir "$STATE" tests/cloud &
sleep 1

# a brand-new process reclaims the runId from disk (first caller to name it)
curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -d "{
  \"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"run/status\",\"params\":{\"runId\":\"$rid\",
    \"_meta\":{\"io.modelcontextprotocol/protocolVersion\":\"2026-07-28\",
               \"io.modelcontextprotocol/clientCapabilities\":{}}}}"
# -> {"state":"failed","step":"Review","resumable":true, ...}   then run/resume as in §3
```

Without `--state-dir` the state dir is per-process and this returns
`unknown runId` after the restart.

## 6. Bounded concurrency

Start with `--max-concurrent-runs 1` and fire three runs:

```bash
for i in 1 2 3; do
  curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "Mcp-Session-Id: $sid" -d "{
    \"jsonrpc\":\"2.0\",\"id\":$i,\"method\":\"run/start\",
    \"params\":{\"name\":\"SlowBuild\",\"arguments\":{\"target\":\"t$i\"}}}" \
  | python3 -c 'import sys,json; r=json.load(sys.stdin)["result"]; print(r["state"], r.get("queuePosition",""))'
done
# -> working
# -> queued 0
# -> queued 1
```

A queued run transitions to `working` as slots free up; `run/status` reports
its `queuePosition` (0 = next up). `run/cancel` on a queued run drops it before
it ever executes. A synchronous `tools/call` does **not** queue — it waits ~5s
for a slot, then returns `-32000` "server at capacity — retry, or use
run/start". This is why the daily workflow should use `run/*`, not `tools/call`.

## 7. Metrics & per-run logs

```bash
curl -s $BASE/metrics
# mhl_serve_runs_total{outcome="completed"} 3
# mhl_serve_run_duration_seconds_sum 12.740
# mhl_serve_runs_active 1
# mhl_serve_runs_queued 2
# mhl_serve_sessions_active 1
# ...
```

`/metrics` is unauthenticated Prometheus text — scrape it from inside the pod
or the mesh. It reports counters (runs by outcome, duration sum/count,
`tools/call` by outcome) and live gauges (`runs_active`, `runs_queued`,
`sessions_active`) read straight off the registry. The `runs_active` /
`runs_queued` gauges are what an HPA should scale on.

Per-run output — the `step:` / `log()` lines — is retrievable through the API,
scoped to the owning session, cursored by byte offset:

```bash
curl -s -X POST $BASE/mcp -H "$JSON" -H "$AUTH" -H "Mcp-Session-Id: $sid" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"run/logs\",\"params\":{\"runId\":\"$rid\"}}"
# -> {"text":"session: ...\nstep: Draft\nstep: Review\n...","nextSince":128}
# poll again with "since": 128 to stream new lines; "dropped":true means the
# 64 KiB ring wrapped past your cursor.
```

The same output also goes to the server's stderr, so `kubectl logs` /
CloudWatch still has it. Lifecycle events (`run started` / `run completed` /
`run queued` / `draining`) are JSON lines on stderr, keyed by `runId` and
`owner`.

---

## Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: mhl-serve }
spec:
  replicas: 1                       # single replica until Phase 3 (shared store)
  template:
    spec:
      terminationGracePeriodSeconds: 40   # a bit above --drain-timeout
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8711"
        prometheus.io/path: /metrics
      containers:
        - name: mhl
          image: <your image with `mhl` + the .mh workflows>
          args:
            - serve
            - mcp
            - --http
            - --addr=0.0.0.0:8711
            - --principal-header=X-Mhl-Principal   # set by the API Gateway authorizer
            - --drain-timeout=30s
            - --max-concurrent-runs=4
            - /workflows
          env:
            - name: MHL_SERVE_TOKEN                 # the gateway↔mhl shared secret
              valueFrom: { secretKeyRef: { name: mhl-serve, key: token } }
            - name: MHL_SERVE_STATE_DIR
              value: /state
          ports: [{ containerPort: 8711 }]
          livenessProbe:
            httpGet: { path: /healthz, port: 8711 }
          readinessProbe:
            httpGet: { path: /readyz, port: 8711 }
            periodSeconds: 5
          volumeMounts:
            - { name: state, mountPath: /state }
      volumes:
        - name: state
          emptyDir: {}               # or a PVC so runs survive a reschedule
```

Put the API Gateway (HTTP API + JWT authorizer + VPC Link) in front. The
authorizer validates the caller's JWT and injects `X-Mhl-Principal` (from `sub`
/ `cognito:groups`) plus the gateway↔mhl bearer; forward `/mcp` **and**
`/mcp/*` to the Service; expose `/healthz` `/readyz` `/metrics` only inside the
cluster.

### Gating `run/resume` without touching mhl

mhl exposes `POST /mcp/run/resume` (and `/mcp/run/start`, …) as its own path, so
the allow/deny decision for "who may approve" lives in the mesh, not a `.mh`
file. An Istio example — only `docs-approvers` may resume:

```yaml
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata: { name: mhl-resume-approvers }
spec:
  selector: { matchLabels: { app: mhl-serve } }
  action: ALLOW
  rules:
    - to: [{ operation: { paths: ["/mcp/run/resume"] } }]
      when: [{ key: request.auth.claims[groups], values: ["docs-approvers"] }]
    - to: [{ operation: { notPaths: ["/mcp/run/resume"] } }]   # everything else: any authenticated caller
```

Equivalent on API Gateway: a separate `POST /mcp/run/resume` route with a
stricter authorizer (or resource-policy statement). The client still sends
`initialize` and everything else to `/mcp`.
