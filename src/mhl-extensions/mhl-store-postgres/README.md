# mhl-store-postgres

An **official** `store`-kind [mhl extension](../../../tests/extensions/extension-protocol.md)
backed by **PostgreSQL**: one row per key in a
`(key text primary key, value jsonb, updated_at timestamptz)` table.

Drop-in replacement for [`store-fs`](../../../tests/extensions/store-fs/) and
[`mhl-store-s3`](../mhl-store-s3/): the same wire contract (kind `store`; `get`
/ `put` / `delete` / `list` over newline-delimited JSON-RPC on stdin/stdout).
`put` is an atomic `INSERT ... ON CONFLICT (key) DO UPDATE`, so a
`mhl serve mcp --http` checkpoint write can never leave a half-written row —
the property `store-fs` and `mhl-store-s3` can't offer.

## Layout

| File | |
|---|---|
| `main.go` | JSON-RPC loop, dispatch, concurrent (goroutine per call) |
| `pg.go` | `pgxpool` pool, `auto_migrate`, `get`/`put`/`delete`/`list` |
| `pg_test.go` | pure-function tests always; live round-trip gated on `MHL_PG_TEST_DSN` |
| `extension.json` / `declarations.json` | manifest + tooling metadata |
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
make dist       # dist/mhl-store-postgres/ — metadata only (extension.json, declarations.json, README.md)
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
- **No CAS / lease / TTL** in the `store` contract v1 — a read-modify-write
  across a `get` then a `put` can still lose updates under concurrency (this is
  a contract limit, not a Postgres one; a `store` v2 CAS method would close it
  trivially here). `mhl serve` sidesteps it with disjoint `run/<id>/…` keys.
- One long-lived pool is shared by every declaration of kind `store`; config is
  pinned from the first call. `mhl serve` refuses more than one `extension store`
  declaration in a workflow directory.
- Each operation has a 30 s client-side context guard on top of any server-side
  `statement_timeout`.
- `mhl extension test .` needs a reachable database — run `make up` first.
