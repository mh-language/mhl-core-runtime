# mhl external extension protocol

Status: v1 (unstable — may change until the runtime declares it frozen)

An **external extension** is any executable that adds a capability to the mhl
runtime without recompiling it. `mhl` speaks to it over **newline-delimited
JSON-RPC on stdin/stdout**. The process is started on first use, kept alive for
the run, multiplexed by request id, and shut down gracefully at the end. It is
never started per method call.

This document is the contract for implementing an extension in any language.
Go authors can lean on `internal/extension/external` for the wire details; other
languages implement the ~4 message shapes below directly.

## Transport

- One JSON object per line on **stdout** (extension → host) and **stdin**
  (host → extension). `\n`-terminated. No framing headers, no batching.
- A line larger than 8 MiB is a protocol violation and the host drops the
  connection.
- **stderr is not part of the protocol.** Write diagnostics there; the host
  captures the tail and quotes it (redacted) into an error if the process
  dies. Never write JSON-RPC to stderr.
- The process inherits **no ambient environment** — only
  `MHL_EXTENSION_API`, `MHL_EXTENSION_ID`, and whatever the manifest's `env`
  lists. Credentials come exclusively through `secret.resolve` (below).

## Message shape

```jsonc
// request  (has id + method)
{ "id": 1, "method": "call", "params": { ... } }
// response (has id, and result xor error, no method)
{ "id": 1, "result": <any json> }
{ "id": 1, "error": { "message": "...", "code": "optional-stable-code" } }
// notification (method, no id)
{ "method": "log", "params": { "message": "..." } }
```

Request ids are host-assigned unsigned integers. Replies may come back in any
order; correlate by `id`. The extension assigns its own ids for the requests
*it* initiates (`secret.resolve`); keep them distinct from nothing in
particular — the host tracks direction, not a shared id space.

## Host → extension

### `initialize` (request, sent once, first)

```json
{ "id": 1, "method": "initialize",
  "params": { "api_version": "1", "host": "mhl", "host_version": "0.5.3" } }
```

Reply:

```json
{ "id": 1, "result": {
    "api_version": "1",
    "extension": { "id": "com.acme.crm", "version": "1.2.0" },
    "declarations": [ /* optional — see below */ ]
} }
```

The host aborts if `result.api_version` differs from its own. `declarations`
is optional: when present it is the extension's self-description, which
`mhl extension package` captures into the manifest's sidecar. At run time the
**manifest**, not the handshake, is what lint and the LSP read.

### `call` (request)

```json
{ "id": 2, "method": "call", "params": {
    "declaration": { "kind": "crm", "name": "Customer", "props": [ { "name": "endpoint", "value": "https://..." } ] },
    "operation": "lookup",
    "args": ["123"],
    "named_args": { }
} }
```

`declaration` is the `extension <kind> <Name> { ... }` block from the `.mh`
program, with every property **already evaluated** to a JSON value. `args` /
`named_args` are the call arguments, likewise evaluated. Reply with
`result` (any JSON value, handed back to the `.mh` caller) or `error`.

### `shutdown` (notification)

```json
{ "method": "shutdown" }
```

Sent once at the end of the run, followed by the host closing stdin. Flush and
exit 0. The host waits ~3s, then sends `SIGKILL`.

## Extension → host

### `log` (notification)

```json
{ "method": "log", "params": { "message": "fetched 12 customers" } }
```

The host writes it to its own output after redaction.

### `secret.resolve` (request)

```json
{ "id": 7, "method": "secret.resolve", "params": { "reference": "env(\"CRM_TOKEN\")" } }
```

Reply from the host: `{ "id": 7, "result": "<value>" }` or
`{ "id": 7, "error": { "code": "denied", "message": "..." } }`. The host only
resolves references the manifest's `permissions.secrets` allow-lists. The
resolved value is registered for redaction — if it later shows up in your
stderr or an error message, the host masks it.

## Manifest — `extension.json`

Required: `id`, `api_version`, `executable`, and at least one declared `kind`
(routing). Everything else is optional.

```json
{
  "id": "com.acme.crm",
  "version": "1.2.0",
  "api_version": "1",
  "executable": "bin/crm",
  "args": [],
  "env": [],
  "declarations": [
    { "kind": "crm",
      "properties": [ { "name": "endpoint", "type": "string" } ],
      "methods": [ { "name": "lookup", "params": [ { "name": "id", "type": "string" } ],
                     "signature": "lookup(id: string) -> object",
                     "documentation": "..." } ] }
  ],
  "permissions": { "network": ["api.acme.com"], "secrets": ["env(\"CRM_TOKEN\")"] }
}
```

`properties` / `methods` are **tooling metadata** (editor completion, hover,
lint hints) — optional, an extension's "debug symbols". Provide them inline, or
in a sidecar named by `declarations_file` (`"declarations_file":
"declarations.json"`, holding the declarations array or `{ "declarations":
[...] }`), or omit them. `declarations` and `declarations_file` are mutually
exclusive.

## Lock — `.mhl/extensions.lock`

```json
{ "extensions": { "com.acme.crm": { "version": "1.2.0", "sha256": "<hex of bin/crm>" } } }
```

An extension present under `.mhl/extensions/<id>/` (or `~/.mhl/extensions/<id>/`)
but **absent from the lock does not load** — the lock is the project's explicit
allow-list. On every run the host re-hashes the executable and refuses a
mismatch. There is no automatic download.

## Tooling

| Command | Does |
|---|---|
| `mhl extension init <dir>` | scaffold a manifest + sidecar + README |
| `mhl extension test <dir>` | spawn, handshake, one arg-free `call` per method — checks the protocol, not the logic |
| `mhl extension package <dir>` | spawn, read `initialize`'s `declarations`, write the sidecar |
| `mhl extension install <dir>` | copy into `.mhl/extensions/<id>/`, hash the executable, pin it in the lock |
| `mhl extension list` / `doctor` | show / validate the locked set |
