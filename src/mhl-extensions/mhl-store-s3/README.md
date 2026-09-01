# mhl-store-s3

An **official** `store`-kind [mhl extension](../../../tests/extensions/extension-protocol.md)
backed by **Amazon S3** — or any S3-compatible object store (MinIO, Cloudflare
R2, Ceph RGW).

It is a drop-in replacement for the reference
[`store-fs`](../../../tests/extensions/store-fs/): the same wire contract (kind
`store`; `get` / `put` / `delete` / `list` over newline-delimited JSON-RPC on
stdin/stdout), storing each key as **one S3 object** at `<prefix><key>.json`.
So a `mhl serve mcp --http` run checkpoint lands at

```
s3://<bucket>/mhl/run/<id>/checkpoint/<pipeline>.json
```

The four operations and the JSON-RPC framing are the whole contract; only the
storage changes versus `store-fs`. SigV4 request signing is implemented here
directly — **no third-party dependencies** (`go.mod` requires nothing).

## Layout

| File | |
|---|---|
| `main.go` | JSON-RPC loop, key mapping, concurrent dispatch |
| `s3.go` | dependency-free S3 client (Put/Get/Delete/ListObjectsV2) + AWS SigV4 + retry |
| `creds.go` | credential sources: static · IRSA web-identity (STS) · IMDSv2 · anonymous |
| `s3_test.go` | pins SigV4 to the AWS documentation's worked example; fake-S3 round trip; retry; IRSA/IMDS |
| `extension.json` / `declarations.json` | manifest + tooling metadata |
| `docker-compose.yml` | local MinIO + bucket bootstrap |
| `smoke.sh` | end-to-end check against local MinIO |

## Build & test

```sh
cd src/mhl-extensions/mhl-store-s3
make build      # -> bin/mhl-store-s3  (host arch; ad-hoc codesigned on macOS)
make test       # go test ./...  (no network; SigV4 vector + fake-S3 round trip)
make vet
make dist       # dist/mhl-store-s3/ — metadata only (extension.json, declarations.json, README.md)
make release    # dist/mhl-store-s3/ + bin/mhl-store-s3-<goos>-<goarch> x5, then dist/release/mhl-store-s3.tar.gz + SHA256SUMS
```

## Local S3 with Docker

```sh
make up         # MinIO on :9000 (API) / :9001 (console); creates bucket `mhl-state`
make smoke      # build + up + end-to-end get/put/delete/list against MinIO
make down       # stop and wipe the data volume
```

Dev credentials baked into `docker-compose.yml` (never reuse anywhere real):

| | |
|---|---|
| endpoint | `http://localhost:9000` |
| access key id | `mhl` |
| secret access key | `mhl-secret-key` |
| bucket | `mhl-state` |
| console | <http://localhost:9001> |

## Use from a project

```sh
# in a project that has a workflow + an `extension store S { ... }` declaration
mhl extension install /path/to/src/mhl-extensions/mhl-store-s3
# or a release archive: mhl extension install https://.../mhl-store-s3.tar.gz#sha256=<hex>
#   install picks the binary for the runtime host and vendors only that one
mhl extension doctor
```

### Against local MinIO

```mhl
extension store S {
    bucket:            "mhl-state"
    endpoint:          "http://localhost:9000"
    region:            "us-east-1"
    prefix:            "mhl/"
    access_key_id:     env("AWS_ACCESS_KEY_ID")
    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
}
```

### Against real Amazon S3

Drop `endpoint` (virtual-host addressing is then used) and set the real region.
STS credentials are supported via `session_token: env("AWS_SESSION_TOKEN")`.

```mhl
extension store S {
    bucket:            "my-company-mhl-state"
    region:            "eu-west-1"
    prefix:            "prod/"
    access_key_id:     env("AWS_ACCESS_KEY_ID")
    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
    session_token:     env("AWS_SESSION_TOKEN")
}
```

`mhl serve mcp --http <dir>` picks the declaration up automatically — `run/*`
checkpoints, `run/*/owner`, and sessions all land in S3; `--state-dir` is then
only a scratch path for the interpreter's own working files.

### On EKS (IRSA / Pod Identity)

The extension process inherits **no ambient environment**, so the usual
`AWS_*` env chain does not apply. Instead pass the projected token path and
role through properties — the runtime (which *does* have the env) resolves the
`env(...)` refs and hands the extension the values; the token *file* is mounted
into the pod and read by the extension:

```mhl
extension store S {
    bucket:                  "my-company-mhl-state"
    region:                  "eu-west-1"
    web_identity_token_file: env("AWS_WEB_IDENTITY_TOKEN_FILE")
    role_arn:                env("AWS_ROLE_ARN")
}
```

The extension calls STS `AssumeRoleWithWebIdentity` and refreshes the
short-lived credentials before they expire. On an EC2/EKS **node** role instead,
set `use_imds: true` (IMDSv2, no env or props needed).

## Properties

| property | default | |
|---|---|---|
| `bucket` | — | **required** |
| `endpoint` | *(AWS)* | S3 endpoint URL; setting it implies path-style addressing |
| `region` | `us-east-1` | SigV4 signing region (must match what the client signs) |
| `prefix` | `mhl/` | key namespace in the bucket; trailing `/` added if missing; `""` = bucket root |
| `access_key_id` / `secret_access_key` | — | **static creds**; use `env(...)` — host-resolved and redacted |
| `session_token` | — | optional STS token for static creds |
| `web_identity_token_file` + `role_arn` | — | **IRSA**: STS AssumeRoleWithWebIdentity, auto-refreshed |
| `role_session_name` | `mhl-store-s3` | IRSA session name |
| `use_imds` | `false` | **node role** via IMDSv2 — only when no static/web-identity creds |
| `force_path_style` | `false` | force bucket-in-path; implied `true` when `endpoint` is set |
| `max_retries` | `3` | retries on transport errors / HTTP 429·5xx, exponential backoff + full jitter; `0` disables |
| `log` | — | optional path for a JSON-lines wire trace (one line per message) |

Credential precedence: **static → web-identity → IMDS → anonymous** (unsigned).
Static/IRSA values are read from properties and resolved **host-side** by the
runtime's credential resolver (`env(...)` / `vault(...)`), which registers them
for redaction. The extension process inherits no ambient environment and never
sees `secret.resolve` traffic — `permissions.secrets` is empty.

## Semantics & limits

- `get` of an absent key returns `null` (S3 `404` → `null`); `delete` of an
  absent key is a no-op (idempotent).
- `list(prefix)` is `ListObjectsV2`, **paginated** (follows
  `NextContinuationToken`), returning logical keys (`<prefix>` and the `.json`
  suffix stripped).
- **No CAS / lease / TTL** — same as `store-fs` and the `store` contract v1.
  Concurrent read-modify-write of the *same* key can lose updates. `mhl serve`
  avoids this by giving each run disjoint `run/<id>/…` keys.
- **Retries**: transport errors and HTTP 429 / 5xx are retried up to
  `max_retries` times with exponential backoff (200 ms base, 5 s cap) + full
  jitter, honouring the call's context deadline. All four operations are
  idempotent, so a retried PUT/DELETE is safe.
- One extension process is shared by every declaration of kind `store`; config
  is pinned from the first call's properties. `mhl serve` refuses more than one
  `extension store` declaration in a workflow directory.
- `mhl extension test .` needs a reachable endpoint (unlike `store-fs`, which is
  pure local FS) — run `make up` first.
