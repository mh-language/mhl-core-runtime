# mhl-store-sqlite

An **official** `store`-kind [mhl extension](../../../tests/extensions/extension-protocol.md)
backed by **SQLite**: durable key/value state for `mhl serve mcp --http`
(sessions, `run/*` checkpoints, `run/*/owner`), one row per key in a
`(key TEXT PRIMARY KEY, value TEXT, updated_at TIMESTAMP)` table inside a
local database file. The driver is [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite)
— pure Go, **no CGO** — so every release target cross-compiles with
`CGO_ENABLED=0`.

Drop-in replacement for [`store-fs`](../../../tests/extensions/store-fs/) and
[`mhl-store-postgres`](../mhl-store-postgres/): the same wire contract (kind
`store`; `get` / `put` / `delete` / `list` over newline-delimited JSON-RPC on
stdin/stdout). `put` is an atomic `INSERT ... ON CONFLICT (key) DO UPDATE`, so
a `mhl serve mcp --http` checkpoint write can never leave a half-written row.
It also implements the **`cas` capability** (`put_if_absent` /
`compare_and_swap`), which lets `mhl serve mcp --http` coordinate `run/*`
execution across replicas.

> **Single-host.** The database is a local file: state is **not** shared
> between machines, and concurrent writers from multiple hosts are not
> possible. For multi-host state use
> [`mhl-store-postgres`](../mhl-store-postgres/) or
> [`mhl-store-redis`](../mhl-store-redis/). Within one host, concurrent
> processes are fine — SQLite's locking plus `busy_timeout_ms` serialise them.

The whole extension is one [`extension.mh`](extension.mh): a `manifest` block
(`id: "dev.mhl.store-sqlite"`, `api_version: "1"`, `executable`,
`permissions` — `network: []`, `secrets: []` — a local file needs neither),
the `properties` schema, and the six method declarations below.

## Methods

| method | |
|---|---|
| `get(key) -> any` | the value at `key`, or `null` when there is no row |
| `put(key, value)` | upsert — `INSERT ... ON CONFLICT (key) DO UPDATE`, atomic |
| `delete(key)` | delete the row; an absent key is not an error |
| `list(prefix) -> [string]` | every key with that prefix (`key LIKE prefix \|\| '%'`, ordered, primary-key index) |
| `put_if_absent(key, value) -> bool` | insert only when no row exists (`INSERT ... ON CONFLICT DO NOTHING`); returns whether the row was created — **`cas`** |
| `compare_and_swap(key, expected, value) -> bool` | replace `value` only when the current value equals `expected` (compared as canonical JSON text); returns whether it swapped — **`cas`** |

The handshake advertises `"capabilities": ["cas"]`. A `store` extension without
it disables cross-replica run locking in `mhl serve` (single-writer only).

`store` is the KV kind `mhl serve mcp --http` routes durable state to, so you
rarely call these directly — a workflow points at the `extension store`
declaration and the runtime does the rest:

```mhl
extension store S {
    path:  env("MHL_STORE_PATH")   # or a literal "state/mhl.db"
    table: "mhl_store"             # default
}
```

## Layout

| File | |
|---|---|
| `main.go` | JSON-RPC loop, dispatch, concurrent (goroutine per call) |
| `sqlite.go` | `database/sql` + `modernc.org/sqlite`, `auto_migrate`, `get`/`put`/`delete`/`list` + `put_if_absent`/`compare_and_swap` |
| `sqlite_test.go` | full suite against a temp database file — no external service needed |
| `extension.mh` | manifest + declarations, single file |
| `smoke.sh` | end-to-end check against a temp database file |

One dependency: `modernc.org/sqlite`. Isolated in this module — the runtime's
`go.mod` is untouched.

## Build & test

```sh
cd src/mhl-extensions/mhl-store-sqlite
make build      # -> bin/mhl-store-sqlite  (host arch; ad-hoc codesigned on macOS)
make test       # full unit + round-trip suite (no service needed)
make vet
make smoke      # build + end-to-end get/put/delete/list + cas via the mhl runtime
make dist       # dist/mhl-store-sqlite/ — metadata only (extension.mh, README.md)
make release    # dist/mhl-store-sqlite/ + bin/mhl-store-sqlite-<goos>-<goarch> x6, then dist/release/mhl-store-sqlite.tar.gz
```

## Use from a project

```sh
mhl extension install /path/to/src/mhl-extensions/mhl-store-sqlite
# or a release archive: mhl extension install https://.../mhl-store-sqlite.tar.gz#sha256=<hex>
mhl extension doctor
```

```mhl
extension store S {
    path: "state/mhl.db"          # required; created on first use (parents too)
    table: "mhl_store"            # default
}
```

`mhl serve mcp --http <dir>` picks the declaration up automatically — `run/*`
checkpoints, `run/*/owner`, and sessions all land in the table; `--state-dir`
is then only a scratch path for the interpreter's own working files.

## Properties

| property | default | |
|---|---|---|
| `path` | — | SQLite database file; **required**. Created on first use, along with any parent directories. Use `env(...)` — host-resolved |
| `table` | `mhl_store` | identifier chars only |
| `prefix` | `""` | optional key namespace inside the table (stored/stripped transparently) |
| `busy_timeout_ms` | `5000` | how long a writer waits on a locked database before `SQLITE_BUSY` |
| `journal_mode` | `WAL` | `DELETE` \| `TRUNCATE` \| `PERSIST` \| `MEMORY` \| `WAL` \| `OFF` |
| `auto_migrate` | `true` | `CREATE TABLE IF NOT EXISTS` on first use; set `false` when the schema is managed externally |
| `log` | — | optional path for a JSON-lines wire trace |

Property values are resolved **host-side** by the runtime's credential
resolver (`env(...)` / `vault(...)`), which registers them for redaction. The
extension process inherits no ambient environment and never sees
`secret.resolve` traffic — `permissions.secrets` is empty.

## Schema

`auto_migrate` runs:

```sql
CREATE TABLE IF NOT EXISTS mhl_store (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

The connection string pins `busy_timeout`, `journal_mode` and
`case_sensitive_like(on)` as `_pragma` parameters, and the pool is capped at
one connection (`SetMaxOpenConns(1)`) — the reliable way to serialise writers
and avoid `SQLITE_BUSY` under concurrent calls.

## Semantics & limits

- `get` of an absent key returns `null`; `delete` of an absent key is a no-op.
- `put` is an atomic upsert — concurrent writers to the same key never corrupt
  a row (last write wins).
- `list(prefix)` is `key LIKE prefix || '%' ORDER BY key`, LIKE-escaped, and
  **case-sensitive** (`case_sensitive_like(on)` — SQLite's LIKE is
  case-insensitive by default, unlike Postgres).
- **`cas`** — `put_if_absent` is one `INSERT ... ON CONFLICT DO NOTHING` and
  `compare_and_swap` one `UPDATE ... WHERE key = ? AND value = ?`. Values are
  stored as compact JSON text and `expected` is canonicalised the same way
  before comparison, so whitespace doesn't matter — the practical equivalent
  of Postgres's jsonb equality. Both atomic, both return whether they took
  effect. `mhl serve` uses them for a per-run execution lock (`run/<id>/lock`).
  There is **no TTL in the store** — the lock's expiry lives in the JSON value
  and is enforced by the runtime; and there is **no fencing token yet**.
- One long-lived connection is shared by every declaration of kind `store`;
  config is pinned from the first call. `mhl serve` refuses more than one
  `extension store` declaration in a workflow directory.
- Each operation has a 30 s client-side context guard.
- **Single-host**: the file is local — state is not shared between machines.
  For multi-host deployments use `mhl-store-postgres` or `mhl-store-redis`.

Release policy, supported platforms, and installation from GitHub assets: [official extensions](../README.md).
