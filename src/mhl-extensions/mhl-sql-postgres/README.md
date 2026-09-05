# mhl-sql-postgres

An **official** `sql`-kind [mhl extension](../../../tests/extensions/extension-protocol.md):
run **free-form data queries (DQL)** against PostgreSQL from a `.mh` workflow
and get rows back as JSON objects.

This is **not** the `store` KV backend — that's
[`mhl-store-postgres`](../mhl-store-postgres/). Different kind (`sql` vs
`store`), so both can be installed at the same time.

```mhl
extension sql Db {
    dsn:       env("DATABASE_URL")
    read_only: true                # default — DQL only
}

pipeline Enrich {
    step pull {
        var users = Db.query(
            "SELECT id, email, plan FROM users WHERE org = $1 AND active = $2",
            orgId, true)
        log(users)   # [{"id":1,"email":"…","plan":"pro"}, …]
    }
}
```

Typical use: an enrichment/validation step that reads context from a database
before an agent call — from a `step`, a `tool` body, or a `before`/`after`
hook. `mhl serve mcp --http` does **not** wire anything to this kind
automatically (only `store` is special); it's a plain capability.

## Methods

| method | |
|---|---|
| `query(sql, ...args) -> [object]` | run a SELECT; each row an object keyed by column name |
| `queryRow(sql, ...args) -> object` | the first row, or `null` |
| `queryValue(sql, ...args) -> any` | first column of the first row, or `null` |
| `exec(sql, ...args) -> number` | one DML/DDL statement, returns affected rows |
| `execScript(sql) -> number` | a **multi-statement** DDL/DML script in **one transaction** (all-or-nothing); no bind params |

The **first argument is the SQL**; the rest bind to `$1, $2, …`. A value is
never interpolated into the SQL text.

`exec` / `execScript` **error unless `read_only: false`** — that switch turns
this into a read-write capability (DML *and* DDL). `read_only: false` also
drops `default_transaction_read_only`, so a normal write transaction is used.

### Deploying DDL

```mhl
extension sql Migrate {
    dsn:       env("DATABASE_URL")
    read_only: false                # required for exec / execScript
}

pipeline Setup {
    step schema {
        Migrate.execScript("
            CREATE TABLE IF NOT EXISTS audit (
                id bigserial PRIMARY KEY,
                at timestamptz NOT NULL DEFAULT now(),
                actor text NOT NULL,
                action text NOT NULL
            );
            CREATE INDEX IF NOT EXISTS audit_at_idx ON audit (at);
        ")
    }
}
```

If any statement fails, the whole script rolls back. Statements that cannot run
in a transaction (`CREATE INDEX CONCURRENTLY`, `VACUUM`, `CREATE DATABASE`) go
through `exec` instead (autocommit).

## "DQL only" — enforced in depth

1. `read_only: true` (default) sets **`default_transaction_read_only = on`** on
   the pool, so PostgreSQL rejects any write — `INSERT`/`UPDATE`/`DELETE`,
   `SELECT … FOR UPDATE`, DDL — with `SQLSTATE 25006`. Bypass-proof, no fragile
   SQL parsing.
2. Every call uses the **extended protocol** (`conn.Query(sql, args…)`), which
   forbids multiple statements in one string (`;`-stacking).
3. Parameters are **always bound**, never concatenated.

Reinforce with a **least-privilege role** (`GRANT SELECT` on the schemas that
matter), `statement_timeout`, and `max_rows`.

## Layout

| File | |
|---|---|
| `main.go` | JSON-RPC loop, dispatch, concurrent (goroutine per call) |
| `sql.go` | `pgxpool`, `query`/`queryRow`/`queryValue`/`exec`, row→JSON normalization |
| `sql_test.go` | pure-fn tests always; live test gated on `MHL_SQL_PG_TEST_DSN` |
| `initdb/seed.sql` | a `people` table for the smoke test / CENARIO scenarios |
| `docker-compose.yml` | seeded Postgres 16 on host `:5434` |

One dependency: `github.com/jackc/pgx/v5`. Isolated module — the runtime's
`go.mod` is untouched.

## Build & test

```sh
cd src/mhl-extensions/mhl-sql-postgres
make build
make test        # pure-function tests (no DB)
MHL_SQL_PG_TEST_DSN=postgres://… go test ./...   # + the live DQL test
make up          # seeded Postgres on :5434
make smoke       # build + up + end-to-end
make down
make dist        # dist/mhl-sql-postgres/ — metadata only (extension.mh, README.md)
make release     # dist/mhl-sql-postgres/ + bin/mhl-sql-postgres-<goos>-<goarch> x5, then dist/release/mhl-sql-postgres.tar.gz + SHA256SUMS
```

## Properties

| property | default | |
|---|---|---|
| `dsn` | — | full connection string; use `env(...)`. Wins over the discrete fields |
| `host` / `port` / `dbname` / `user` | — | used when `dsn` is unset |
| `password` | — | use `env(...)` — host-resolved and redacted |
| `sslmode` | `prefer` | `disable` \| `require` \| `verify-full` … |
| `read_only` | `true` | `default_transaction_read_only=on` + `exec` disabled; set `false` to allow DML |
| `max_rows` | `10000` | a `query` returning more errors (add a `LIMIT`); `0` = unlimited |
| `max_conns` | `4` | connection-pool size |
| `statement_timeout` | *(server default)* | per-statement, as a Go duration |
| `log` | — | JSON-lines wire trace: SQL head + arg **count**, never values |

## Type mapping (PostgreSQL → JSON)

`pgx` yields Go natives / `pgtype` values that already marshal sensibly; the
normalizer only fixes what JSON gets wrong:

| PostgreSQL | JSON |
|---|---|
| int / float / bool / text | native |
| `numeric` | number (trailing zeros not preserved) |
| `timestamptz` / `timestamp` / `date` | RFC3339 string |
| `jsonb` / `json` | nested value |
| `uuid` | hyphenated string |
| `bytea` | base64 string |
| arrays | JSON array |
| `NULL` | `null` |

Exotic types (interval, range, composite, custom) marshal via their `pgtype`
codec or become a string.

## Limits

- Autocommit per call — no transaction spanning steps in v1.
- `query` buffers all rows in memory (bounded by `max_rows`); not a stream.
- One long-lived pool shared by every `sql` declaration; config pinned from the
  first call. `mhl` refuses two extensions serving the same kind, but two
  *declarations* of this one share the pool + first-call config.
- `mhl extension test .` needs a reachable database — run `make up` first.
