# mhl-cache-redis

An **official** `cache`-kind [mhl extension](../../../tests/extensions/extension-protocol.md)
backed by **Redis**: TTL-first key/value caching for `.mh` workflows.

`cache` is a **different kind** from `store` ([`mhl-store-postgres`](../mhl-store-postgres/))
and `sql` ([`mhl-sql-postgres`](../mhl-sql-postgres/)), so all of them can be
installed at once. It is a **plain capability** — `mhl serve mcp --http` does
not route anything to it automatically; you call it from a `step`, a `tool`
body, or a `before`/`after` hook.

```mhl
extension cache C {
    url:        env("REDIS_URL")     # redis://[user:pass@]host:port/db  (rediss:// = TLS)
    ttl:        "5m"                  # default expiry for set() without its own
    key_prefix: "app:"
}

pipeline P {
    step warm {
        var hit = C.get("user:42")
        if (hit == null) {
            C.set("user:42", { name: "Ana" }, "10m")
        }
    }
    step rate {
        var n = C.incr("calls:2026-08-31")   # atomic counter — no lost update
    }
}
```

## Methods

| method | |
|---|---|
| `get(key) -> any` | JSON-decoded value, or `null` when absent |
| `set(key, value[, ttl])` | store `value` (JSON); `ttl` overrides the declaration default |
| `delete(key)` | remove key (absent is fine) |
| `has(key) -> bool` | `EXISTS` |
| `incr(key) -> number` | atomic `+1`, returns the new value |
| `incrBy(key, n) -> number` | atomic `+n` |
| `expire(key, ttl) -> bool` | set/refresh expiry; `false` if the key is gone |
| `ttl(key) -> number` | remaining seconds (`-1` no expiry, `-2` missing) |

`ttl` is `"30s"` / `"5m"` / `"1h"`, or a plain number of seconds.

## Layout

| File | |
|---|---|
| `main.go` | JSON-RPC loop, dispatch, concurrent (goroutine per call) |
| `resp.go` | dependency-free RESP2 client + connection pool (GET/SET/DEL/EXISTS/INCR/INCRBY/EXPIRE/TTL, AUTH/SELECT/PING) |
| `cache.go` | JSON encoding, key prefix, default TTL, the 8 methods |
| `resp_test.go` | RESP encode/decode, TTL/URL parsing; live test gated on `MHL_REDIS_TEST_ADDR` |
| `docker-compose.yml` | Redis 7 on host `:6380` |

**Zero dependencies** — `go.mod` requires nothing (RESP2 is small enough to
own, unlike a SQL wire protocol).

## Build & test

```sh
cd src/mhl-extensions/mhl-cache-redis
make build
make test       # RESP + parsing unit tests (no server)
MHL_REDIS_TEST_ADDR=localhost:6380 go test ./...   # + the live test
make up          # Redis on :6380
make smoke       # build + up + end-to-end
make down
make dist        # dist/mhl-cache-redis/ — metadata only (extension.json, declarations.json, README.md)
make release     # dist/mhl-cache-redis/ + bin/mhl-cache-redis-<goos>-<goarch> x5, then dist/release/mhl-cache-redis.tar.gz + SHA256SUMS
```

## Properties

| property | default | |
|---|---|---|
| `url` | — | `redis://[user:pass@]host:port/db`; `rediss://` = TLS. Use `env(...)`. Wins over the discrete fields |
| `addr` | `localhost:6379` | `host:port` when `url` is unset |
| `username` / `password` | — | ACL user + `AUTH`; use `env(...)` (redacted) |
| `db` | `0` | logical database (`SELECT`) |
| `tls` / `tls_skip_verify` | `false` | TLS for managed Redis; skip-verify for dev |
| `ttl` | *(none)* | default expiry for `set()` — `"30s"` / `"5m"` / seconds; unset = keys never expire |
| `key_prefix` | `""` | prepended to every key |
| `pool_size` | `8` | max idle pooled connections |
| `dial_timeout` / `read_timeout` | `5s` / `3s` | Go durations |
| `log` | — | JSON-lines wire trace: op + key, **never values** |

## Semantics & notes

- Values are JSON-encoded strings. `incr`/`incrBy` use Redis integers — call
  them only on keys holding an integer (a fresh key starts at 0; a JSON object
  errors).
- `incr` / `incrBy` are **atomic server-side** — concurrent increments never
  lose an update (contrast the read-modify-write lost-update in the `store`
  scenarios). Use them for rate limits, dedup counters, fan-in tallies.
- `set` uses `PX` (millisecond expiry) so `"500ms"` works; `expire` is
  second-granularity (rounded up, min 1s).
- One small connection pool is shared by every `cache` declaration; config is
  pinned from the first call. A dropped connection is retried once with a fresh
  one; a Redis-level error (`-WRONGTYPE`, …) is returned, not retried.
- `mhl extension test .` needs a reachable Redis — run `make up` first.
- Not durable state: point run checkpoints at [`mhl-store-postgres`](../mhl-store-postgres/)
  or [`mhl-store-s3`](../mhl-store-s3/), not here.
