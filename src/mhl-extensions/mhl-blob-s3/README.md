# mhl-blob-s3

An **official** `blob`-kind [mhl extension](../../../tests/extensions/extension-protocol.md)
backed by **Amazon S3** — or any S3-compatible object store (MinIO, Cloudflare
R2, Ceph RGW).

It is a general-purpose object-storage capability a `.mh` workflow reaches for
directly: upload and download objects (text or binary), list a prefix, HEAD for
metadata, server-side copy, and mint time-limited **presigned URLs**. SigV4
request signing (header form for the extension's own calls, query form for
presigned URLs) is implemented here directly — **no third-party dependencies**
(`go.mod` requires nothing).

> This is **not** the `store` KV backend for `mhl serve mcp --http` durable
> state. For that use [`mhl-store-postgres`](../mhl-store-postgres/) or
> [`mhl-store-redis`](../mhl-store-redis/).

## Methods

| method | returns | |
|---|---|---|
| `put(key, body, content_type?)` | `void` | upload UTF-8 text, overwriting |
| `put_bytes(key, base64, content_type?)` | `void` | upload binary content given base64 |
| `get(key)` | `string \| null` | object body as text, `null` when absent |
| `get_bytes(key)` | `string \| null` | object body base64-encoded, `null` when absent |
| `head(key)` | `object \| null` | `{ key, size, etag, content_type, last_modified }`, no body downloaded |
| `exists(key)` | `bool` | whether the object exists (a HEAD) |
| `delete(key)` | `void` | remove; an absent key is not an error |
| `list(prefix, delimiter?)` | `object` | `{ keys: [{ key, size, etag, last_modified }], common_prefixes: [string] }` |
| `copy(src, dst)` | `void` | server-side copy within the same bucket (no download) |
| `presign_get(key, expires)` | `string` | time-limited GET URL; `expires` is `"15m"`/`"1h"` or seconds (max 7 days) |
| `presign_put(key, expires, content_type?)` | `string` | time-limited PUT URL; a given `content_type` is pinned into the signature |

`key` is always relative to `prefix` — the prefix is prepended on the way in and
stripped from every key `list`/`head` reports. A non-empty `delimiter` (e.g.
`"/"`) rolls sub-"folders" into `common_prefixes` instead of listing their
contents.

## Layout

| File | |
|---|---|
| `main.go` | JSON-RPC loop, key mapping, method dispatch, presign expiry parsing |
| `s3.go` | dependency-free S3 client (Put/Get/Head/Delete/Copy/ListObjectsV2) + SigV4 (header & query form) + retry |
| `creds.go` | credential sources: static · IRSA web-identity (STS) · IMDSv2 · anonymous |
| `s3_test.go` | pins SigV4 to the AWS worked example; fake-S3 round trip incl. HEAD/copy/delimiter; presigned query form; retry; IRSA/IMDS |
| `extension.mh` | manifest + declarations, single file (`extensible blob`) |
| `docker-compose.yml` | local MinIO + bucket bootstrap |
| `smoke.sh` | end-to-end check against local MinIO |

## Build & test

```sh
cd src/mhl-extensions/mhl-blob-s3
make build      # -> bin/mhl-blob-s3  (host arch; ad-hoc codesigned on macOS)
make test       # go test ./...  (no network; SigV4 vectors + fake-S3 round trip)
make vet
make dist       # dist/mhl-blob-s3/ — metadata only (extension.mh, README.md)
make release    # dist/mhl-blob-s3/ + bin/mhl-blob-s3-<goos>-<goarch> x6, then dist/release/mhl-blob-s3.tar.gz
```

## Local S3 with Docker

```sh
make up         # MinIO on :9000 (API) / :9001 (console); creates bucket `mhl-blob`
make smoke      # build + up + end-to-end put/get/head/exists/list/copy/delete/presign against MinIO
make down       # stop and wipe the data volume
```

Dev credentials baked into `docker-compose.yml` (never reuse anywhere real):

| | |
|---|---|
| endpoint | `http://localhost:9000` |
| access key id | `mhl` |
| secret access key | `mhl-secret-key` |
| bucket | `mhl-blob` |
| console | <http://localhost:9001> |

## Use from a project

```sh
# in a project that has a workflow + an `extension blob Files { ... }` declaration
mhl extension install /path/to/src/mhl-extensions/mhl-blob-s3
# or a release archive: mhl extension install https://.../mhl-blob-s3.tar.gz#sha256=<hex>
#   install picks the binary for the runtime host and vendors only that one
mhl extension doctor
```

### Against local MinIO

```mhl
extension blob Files {
    provider:          "s3"
    bucket:            "mhl-blob"
    endpoint:          "http://localhost:9000"
    region:            "us-east-1"
    prefix:            "artifacts/"
    access_key_id:     env("AWS_ACCESS_KEY_ID")
    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
}

pipeline Publish {
    step upload {
        Files.put("reports/q3.csv", render_csv(), "text/csv")
    }
    step share {
        var url = Files.presign_get("reports/q3.csv", "1h")
        log("download: ${url}")
    }
}
```

### Against real Amazon S3

Drop `endpoint` (virtual-host addressing is then used) and set the real region.
STS credentials are supported via `session_token: env("AWS_SESSION_TOKEN")`.

```mhl
extension blob Files {
    provider:          "s3"
    bucket:            "my-company-artifacts"
    region:            "eu-west-1"
    prefix:            "prod/"
    access_key_id:     env("AWS_ACCESS_KEY_ID")
    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
    session_token:     env("AWS_SESSION_TOKEN")
}
```

### On EKS (IRSA / Pod Identity)

The extension process inherits **no ambient environment**, so the usual
`AWS_*` env chain does not apply. Instead pass the projected token path and
role through properties — the runtime (which *does* have the env) resolves the
`env(...)` refs and hands the extension the values; the token *file* is mounted
into the pod and read by the extension:

```mhl
extension blob Files {
    provider:                "s3"
    bucket:                  "my-company-artifacts"
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
| `provider` | `s3` | object-store provider; only `s3` is implemented — reserved so a future mhl-blob-gcs/azure can share the contract |
| `bucket` | — | **required** |
| `endpoint` | *(AWS)* | S3 endpoint URL; setting it implies path-style addressing |
| `region` | `us-east-1` | SigV4 signing region (must match what the client signs) |
| `prefix` | `""` | key namespace in the bucket; trailing `/` added if missing; `""` = bucket root |
| `access_key_id` / `secret_access_key` | — | **static creds**; use `env(...)` — host-resolved and redacted |
| `session_token` | — | optional STS token for static creds |
| `web_identity_token_file` + `role_arn` | — | **IRSA**: STS AssumeRoleWithWebIdentity, auto-refreshed |
| `role_session_name` | `mhl-blob-s3` | IRSA session name |
| `use_imds` | `false` | **node role** via IMDSv2 — only when no static/web-identity creds |
| `force_path_style` | `false` | force bucket-in-path; implied `true` when `endpoint` is set |
| `max_retries` | `3` | retries on transport errors / HTTP 429·5xx, exponential backoff + full jitter; `0` disables |
| `log` | — | optional path for a JSON-lines wire trace (op + key, never bodies) |

Credential precedence: **static → web-identity → IMDS → anonymous** (unsigned).
Static/IRSA values are read from properties and resolved **host-side** by the
runtime's credential resolver (`env(...)` / `vault(...)`), which registers them
for redaction. The extension process inherits no ambient environment and never
sees `secret.resolve` traffic — `permissions.secrets` is empty.

## Semantics & limits

- `get` / `get_bytes` / `head` of an absent key return `null` (S3 `404` →
  `null`); `delete` of an absent key is a no-op (idempotent).
- `list(prefix, delimiter)` is `ListObjectsV2`, **paginated** (follows
  `NextContinuationToken`). With a non-empty `delimiter` the response's
  `CommonPrefixes` are returned in `common_prefixes` (prefix-stripped) and keys
  below them are not enumerated.
- `copy` is a server-side `x-amz-copy-source` PUT — no bytes transit the
  extension. Source and destination are both within `bucket`.
- `presign_get` / `presign_put` build a **query-form SigV4** URL signed with the
  current credentials; the URL is valid for `expires` (capped at 7 days, the
  SigV4 maximum) and needs no further auth. `presign_put` with a `content_type`
  binds `Content-Type` into the signature, so the uploader must send exactly
  that header.
- **No CAS / lease / TTL / versioning surface** — this is plain object storage.
  Concurrent writers to the *same* key race with last-write-wins.
- **Retries**: transport errors and HTTP 429 / 5xx are retried up to
  `max_retries` times with exponential backoff (200 ms base, 5 s cap) + full
  jitter, honouring the call's context deadline. Every operation is idempotent,
  so a retried PUT/DELETE/COPY is safe.
- One extension process is shared by every `extension blob` declaration; config
  is pinned from the first call's properties.
- `mhl extension test .` needs a reachable endpoint — run `make up` first.

Release policy, supported platforms, and installation from GitHub assets: [official extensions](../README.md).
