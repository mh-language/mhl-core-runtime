# extensions

External extensions let a project add capabilities the runtime does not ship —
a CRM connector, a house database client, anything — as a **separate executable**
that `mhl` talks to over newline-delimited JSON-RPC on stdin/stdout. The process
is started on first use, kept alive for the run, multiplexed by request id, and
shut down gracefully at the end. It is never started per method call.

Unlike the examples under `syntax/` and `features/`, there is nothing to
`mhl test` here — an external extension needs a real compiled binary. This
directory documents the two files that wire one into a project. The end-to-end
behaviour is covered by `src/mhl-runtime/internal/extension/external` and
`src/mhl-runtime/internal/cli`'s tests.

## Layout

```
my-project/
├── main.mh
└── .mhl/
    ├── extensions.lock                     # the project's allow-list (pinned)
    └── extensions/
        └── com.acme.crm/
            ├── extension.json              # the manifest
            └── bin/crm                     # the executable
```

`.mhl/extensions/<id>/` may also live under `~/.mhl/extensions/<id>/` (user-wide
install); the project-local copy wins.

## Manifest — `extension.json`

Only four things are **required**: `id`, `api_version`, `executable`, and at
least one declared `kind` (that's the routing — it's how `extension crm X {}`
finds this binary). The smallest valid manifest:

```json
{
  "id": "com.acme.crm",
  "api_version": "1",
  "executable": "bin/crm",
  "declarations": [{ "kind": "crm" }]
}
```

`properties` and `methods` inside a declaration are **optional tooling
metadata** — an extension's "debug symbols". They drive editor completion,
signature help and lint hints; leaving them out only makes the editor
experience thinner, the runtime and `mhl extension doctor` are unaffected.
Fully described:

```json
{
  "id": "com.acme.crm",
  "version": "1.2.0",
  "api_version": "1",
  "executable": "bin/crm",
  "declarations": [
    {
      "kind": "crm",
      "properties": [{ "name": "endpoint", "type": "string" }],
      "methods": [
        {
          "name": "lookup",
          "params": [{ "name": "id", "type": "string" }],
          "signature": "lookup(id: string) -> object",
          "documentation": "Fetch a customer by id."
        }
      ]
    }
  ],
  "permissions": {
    "network": ["api.acme.com"],
    "secrets": ["env(\"CRM_TOKEN\")"]
  }
}
```

Or keep the descriptions in a **sidecar** (the "portable symbols" form — the
SDK can regenerate it without touching the manifest):

```json
{
  "id": "com.acme.crm",
  "api_version": "1",
  "executable": "bin/crm",
  "declarations_file": "crm.d.json"
}
```

where `crm.d.json` is the declarations array (or `{ "declarations": [ ... ] }`).
Set `declarations` **or** `declarations_file`, not both.

Whatever form is used, it is read by `mhl lint` and the LSP **without spawning
the process**. `permissions.secrets` is the allow-list for `secret.resolve`:
the extension process starts with **no inherited environment** and must ask the
host for each credential it is permitted here.

## Lock — `.mhl/extensions.lock`

```json
{
  "extensions": {
    "com.acme.crm": {
      "version": "1.2.0",
      "sha256": "<hex sha256 of bin/crm>"
    }
  }
}
```

An extension present on disk but **absent from the lock does not load**. On
every run the host re-hashes the executable and refuses it if the hash drifts
from the pin. There is no automatic download — vendor the extension and edit the
lock by hand until a trust policy exists.

## Using it from `.mh`

```mh
extension crm Customer {
    endpoint: env("CRM_URL")
}

pipeline sync {
    step load {
        var c = Customer.lookup("123")
        log(c)
    }
}
```

## Commands

- `mhl extension init <dir>` — scaffold a manifest + `declarations.json` + README
- `mhl extension test <dir>` — spawn the extension, handshake, one argument-free
  `call` per declared method — checks it speaks the protocol, not the logic
- `mhl extension package <dir>` — spawn, read the `declarations` the extension
  reports in its handshake, write them to the sidecar
- `mhl extension install <dir>` — copy the extension into
  `.mhl/extensions/<id>/`, hash the executable, pin it in the lock
- `mhl extension list` — every entry in the lock and whether it resolves
- `mhl extension doctor` — validate each (manifest, executable bit, hash,
  api_version); non-zero exit if any is broken

The full wire protocol is specified in
[docs/extension-protocol.md](../../docs/extension-protocol.md) — implement it in
any language.
