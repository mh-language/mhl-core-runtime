# mhl-store-postgres

An **official** `store`-kind [mhl extension](../../../tests/extensions/extension-protocol.md)
backed by **PostgreSQL**: durable key/value state for `mhl serve mcp --http`
(sessions, `run/*` checkpoints, `run/*/owner`), one row per key in a
`(key text primary key, value jsonb, updated_at timestamptz)` table.

Drop-in replacement for [`store-fs`](../../../tests/extensions/store-fs/) and
[`mhl-store-s3`](../mhl-store-s3/): the same wire contract (kind `store`; `get`
/ `put` / `delete` / `list` over newline-delimited JSON-RPC on stdin/stdout).
`put` is an atomic `INSERT ... ON CONFLICT (key) DO UPDATE`, so a
`mhl serve mcp --http` checkpoint write can never leave a half-written row —
the property `store-fs` and `mhl-store-s3` can't offer. It also implements the
**`cas` capability** (`put_if_absent` / `compare_and_swap`), which lets
`mhl serve mcp --http` coordinate `run/*` execution across replicas.

The whole extension is one [`extension.mh`](extension.mh): a `manifest` block
(`id: "dev.mhl.store-postgres"`, `api_version: "1"`, `executable`,
`permissions` — `network: ["*"]`, `secrets: []`), the `properties` schema, and
the six method declarations below.

## Methods

| method | |
|---|---|
| `get(key) -> any` | the value at `key`, or `null` when there is no row |
| `put(key, value)` | upsert — `INSERT ... ON CONFLICT (key) DO UPDATE`, atomic |
| `delete(key)` | delete the row; an absent key is not an error |
| `list(prefix) -> [string]` | every key with that prefix (`key LIKE prefix \|\| '%'`, ordered, index-backed) |
| `put_if_absent(key, value) -> bool` | insert only when no row exists (`INSERT ... ON CONFLICT DO NOTHING`); returns whether the row was created — **`cas`** |
| `compare_and_swap(key, expected, value) -> bool` | replace `value` only when the current value equals `expected` under jsonb equality; returns whether it swapped — **`cas`** |

The handshake advertises `"capabilities": ["cas"]`. A `store` extension without
it disables cross-replica run locking in `mhl serve` (single-writer only).

`store` is the KV kind `mhl serve mcp --http` routes durable state to, so you
rarely call these directly — a workflow points at the `extension store`
declaration and the runtime does the rest:

```mhl
extension store S {
    dsn:   env("DATABASE_URL")   # or the discrete host/port/dbname/user fields
    table: "mhl_store"           # default; may be "schema.table"
}
```

## Layout

| File | |
|---|---|
| `main.go` | JSON-RPC loop, dispatch, concurrent (goroutine per call) |
| `pg.go` | `pgxpool` pool, `auto_migrate`, `get`/`put`/`delete`/`list` + `put_if_absent`/`compare_and_swap` |
| `pg_test.go` | pure-function tests always; live round-trip gated on `MHL_PG_TEST_DSN` |
| `extension.mh` | manifest + declarations, single file |
| `docker-compose.yml` | local Postgres 16 |
| `smoke.sh` | end-to-end check against local Postgres |

One dependency: `github.com/jackc/pgx/v5` (+ its handful of small `jackc/*`
and `golang.org/x/*` modules). Isolated in this module — the runtime's
`go.mod` is untouched.

## Build & test

```sh
cd src/mhl-extensions/mhl-store-postgres
make build      # -> bin/mhl-store-postgres  (host arch; ad-hoc codesigned on macOS)
make test       # pure-function unit tests (no DB)
MHL_PG_TEST_DSN=postgres://... go test ./...   # + the live round trip
make vet
make dist       # dist/mhl-store-postgres/ — metadata only (extension.mh, README.md)
make release    # dist/mhl-store-postgres/ + bin/mhl-store-postgres-<goos>-<goarch> x5, then dist/release/mhl-store-postgres.tar.gz + SHA256SUMS
```

## Local Postgres with Docker

```sh
make up         # Postgres 16 on host :5433 -> container 5432, waits for healthy
make smoke      # build + up + end-to-end get/put/delete/list
make down       # stop and wipe the data volume
```

Dev credentials baked into `docker-compose.yml` (never reuse anywhere real):

| | |
|---|---|
| dsn | `postgres://mhl:mhl-secret-pw@localhost:5433/mhl_state?sslmode=disable` |
| user / password | `mhl` / `mhl-secret-pw` |
| dbname | `mhl_state` |

## Use from a project

```sh
mhl extension install /path/to/src/mhl-extensions/mhl-store-postgres
# or a release archive: mhl extension install https://.../mhl-store-postgres.tar.gz#sha256=<hex>
mhl extension doctor
```

```mhl
extension store S {
    dsn:    env("DATABASE_URL")       # or the discrete fields below
    table:  "mhl_store"               # default; may be "schema.table"
}
```

Or discrete fields instead of a DSN:

```mhl
extension store S {
    host:     "db.internal"
    port:     "5432"
    dbname:   "mhl_state"
    user:     "mhl"
    password: env("PGPASSWORD")
    sslmode:  "require"
}
```

`mhl serve mcp --http <dir>` picks the declaration up automatically — `run/*`
checkpoints, `run/*/owner`, and sessions all land in the table; `--state-dir`
is then only a scratch path for the interpreter's own working files.

## Properties

| property | default | |
|---|---|---|
| `dsn` | — | full connection string (URL or libpq keyword/value); wins over the discrete fields. Use `env(...)` |
| `host` / `port` / `dbname` / `user` | — | used when `dsn` is unset |
| `password` | — | use `env(...)` — host-resolved and redacted |
| `sslmode` | `prefer` | `disable` \| `require` \| `verify-ca` \| `verify-full` … |
| `table` | `mhl_store` | identifier chars only; may be `schema.table` |
| `prefix` | `""` | optional key namespace inside the table (stored/stripped transparently) |
| `max_conns` | `8` | connection-pool size (`MinConns` 0, idle released after 5 min) |
| `statement_timeout` | *(server default)* | per-statement, as a Go duration (`"10s"`, `"500ms"`) |
| `auto_migrate` | `true` | `CREATE TABLE / INDEX IF NOT EXISTS` on first use; set `false` when the schema is managed externally |
| `log` | — | optional path for a JSON-lines wire trace |

Credentials are read from properties and resolved **host-side** by the
runtime's credential resolver (`env(...)` / `vault(...)`), which registers them
for redaction. The extension process inherits no ambient environment and never
sees `secret.resolve` traffic — `permissions.secrets` is empty. (`resolveStoreProps`
allows one `env()`/`vault()` per property and no string interpolation, so pass a
whole `dsn: env("DATABASE_URL")` or the discrete fields — not
`"postgres://u:${...}@h/db"`.)

## Schema

`auto_migrate` runs:

```sql
CREATE TABLE IF NOT EXISTS mhl_store (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS mhl_store_key_pattern_idx ON mhl_store (key text_pattern_ops);
```

The `text_pattern_ops` index makes `list(prefix)` (`key LIKE prefix || '%'`)
indexable regardless of the database collation.

## Semantics & limits

- `get` of an absent key returns `null`; `delete` of an absent key is a no-op.
- `put` is an atomic upsert — concurrent writers to the same key never corrupt
  a row (last write wins).
- `list(prefix)` is `key LIKE prefix || '%' ORDER BY key`, LIKE-escaped.
- **`cas`** — `put_if_absent` is one `INSERT ... ON CONFLICT DO NOTHING` and
  `compare_and_swap` one `UPDATE ... WHERE key = $1 AND value = $expected::jsonb`
  (jsonb equality: whitespace and key order don't matter). Both atomic, both
  return whether they took effect. `mhl serve` uses them for a per-run
  execution lock (`run/<id>/lock`), so a plain `get`-then-`put` race is no
  longer the only option. There is **no TTL in the store** — the lock's expiry
  lives in the JSON value and is enforced by the runtime; and there is **no
  fencing token yet** (a resurrected holder that lost the lock could still
  write a checkpoint — a later `store` addition).
- One long-lived pool is shared by every declaration of kind `store`; config is
  pinned from the first call. `mhl serve` refuses more than one `extension store`
  declaration in a workflow directory.
- Each operation has a 30 s client-side context guard on top of any server-side
  `statement_timeout`.
- `mhl extension test .` needs a reachable database — run `make up` first.
