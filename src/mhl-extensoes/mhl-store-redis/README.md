# mhl-store-redis

An **official** `store`-kind [mhl extension](../../../tests/extensions/extension-protocol.md)
backed by **Redis**: durable key/value state for `mhl serve mcp --http`
(sessions, `run/*` checkpoints, `run/*/owner`, `run/*/lock`), one Redis key
per key, JSON values, **no TTL**.

Drop-in replacement for [`store-fs`](../../../tests/extensions/store-fs/),
[`mhl-store-postgres`](../mhl-store-postgres/) and
[`mhl-store-s3`](../mhl-store-s3/): the same wire contract (kind `store`; `get`
/ `put` / `delete` / `list` over newline-delimited JSON-RPC on stdin/stdout).
It also implements the **`cas` capability** (`put_if_absent` /
`compare_and_swap`), which lets `mhl serve mcp --http` coordinate `run/*`
execution across replicas — one replica per `runId` at a time.

Sibling: [`mhl-cache-redis`](../mhl-cache-redis/) is the `cache`-kind Redis
extension (TTL, `incr`, `expire`) — a different kind, installable alongside.

The whole extension is one [`extension.mh`](extension.mh): a `manifest` block
(`id: "dev.mhl.store-redis"`, `api_version: "1"`, `executable`, `permissions` —
`network: ["*"]`, `secrets: []`), the `properties` schema, and the six method
declarations below. Dependency-free RESP2 client — the module's `go.mod` has
zero requires.

## Methods

| method | |
|---|---|
| `get(key) -> any` | the JSON value at `key`, or `null` when the key is absent |
| `put(key, value)` | store `value` (JSON) at `key`, overwriting (`SET`) |
| `delete(key)` | remove the key; an absent key is not an error |
| `list(prefix) -> [string]` | every key with that prefix, **sorted** (`SCAN MATCH prefix* COUNT 250` in a cursor loop, glob-escaped) |
| `put_if_absent(key, value) -> bool` | store only when the key does not exist (`SET ... NX`); returns whether it was created — **`cas`** |
| `compare_and_swap(key, expected, value) -> bool` | replace `value` only when the current value is byte-equal to `expected` (one atomic Lua `EVAL`); returns whether it swapped — **`cas`** |

The handshake advertises `"capabilities": ["cas"]`. A `store` extension without
it disables cross-replica run locking in `mhl serve` (single-writer only).

```mhl
extension store S {
    url:   env("REDIS_URL")   # redis://[user:pass@]host:port/db (rediss:// = TLS)
    # or: addr / username / password / db / tls
}
```

`mhl serve mcp --http <dir>` picks the declaration up automatically — `run/*`
checkpoints, `run/*/owner`, `run/*/lock` and sessions all land in Redis;
`--state-dir` is then only a scratch path for the interpreter's own working
files.

## Layout

| File | |
|---|---|
| `main.go` | JSON-RPC loop, dispatch, concurrent (goroutine per call) |
| `store.go` | `redisStore` — `get`/`put`/`delete`/`list` + `put_if_absent`/`compare_and_swap` |
| `resp.go` | tiny RESP2 client (encode/parse, pool, GET/SET/DEL/SCAN/EVAL) |
| `resp_test.go` | pure-function tests always; live round-trip gated on `MHL_REDIS_TEST_ADDR` |
| `extension.mh` | manifest + declarations, single file |
| `docker-compose.yml` | local Redis 7 on host `:6381` |
| `smoke.sh` | end-to-end check against local Redis |

## Build & test

```sh
cd src/mhl-extensoes/mhl-store-redis
make build      # -> bin/mhl-store-redis  (host arch; ad-hoc codesigned on macOS)
make test       # pure-function unit tests (no Redis)
MHL_REDIS_TEST_ADDR=localhost:6381 go test ./...   # + the live round trip
make vet
make dist       # dist/mhl-store-redis/ — metadata only (extension.mh, README.md)
make release    # + bin/mhl-store-redis-<goos>-<goarch> x5, then dist/release/mhl-store-redis.tar.gz
```

## Local Redis with Docker

```sh
make up         # Redis 7 on host :6381 -> container 6379, waits for healthy
make smoke      # build + up + end-to-end get/put/delete/list + cas
make down       # stop and wipe
```

`url = redis://localhost:6381/0`

## Semantics & limits

- `get` of an absent key returns `null`; `delete` of an absent key is a no-op.
- `put` is a plain `SET` (last write wins). No TTL — store keys are durable.
- `list(prefix)` walks `SCAN MATCH prefix*` and sorts the result, so it is
  `O(keyspace)` — fine for `run/*` / `session/*` cardinality, not a general
  index.
- **`cas`** — `put_if_absent` is `SET k v NX`; `compare_and_swap` is a Lua
  `EVAL` doing `GET`-compare-then-`SET` atomically (byte equality on the
  stored value). Both return whether they took effect. `mhl serve` uses them
  for a per-run execution lock (`run/<id>/lock`). There is **no TTL in the
  store** — the lock's expiry lives in the JSON value and the runtime enforces
  it — and **no fencing token yet** (a resurrected holder that lost the lock
  could still write a checkpoint; a later addition).
- One long-lived pool; config pinned from the first call. `mhl serve` refuses
  more than one `extension store` declaration per workflow directory.
- Each operation has a 3 s read/write timeout (`read_timeout`) plus a 5 s dial
  timeout (`dial_timeout`), both overridable.
- `mhl extension test .` needs a reachable Redis — run `make up` first.
