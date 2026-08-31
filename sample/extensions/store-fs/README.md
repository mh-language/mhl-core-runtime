# store-fs — a reference `store` extension

The key/value backend `mhl serve mcp --http` uses for **durable run and session
state** when the workflow directory declares one, instead of the on-disk
`.mhl/state` tree. This one keeps each key as a JSON file mirrored under `dir`
— trivial to inspect, not for production. A real backend (DynamoDB, Redis,
Postgres) swaps the storage; the four operations and the wire protocol are the
whole contract.

## Contract

Kind `store`, one property (`dir`), four methods:

| method | |
|---|---|
| `get(key) -> any` | the value, or `null` when absent |
| `put(key, value)` | store `value` at `key`, overwriting |
| `delete(key)` | remove `key` (absent is fine) |
| `list(prefix) -> [string]` | every key with that prefix |

Keys the server writes: `session/<sid>`, `run/<id>/checkpoint/<pipeline>`,
`run/<id>/result`, `run/<id>/owner`. It never assumes ordering, TTL, or
transactions in 3a; CAS / lease come with distributed run execution (Phase 4).

## Build &amp; use

```sh
cd sample/extensions/store-fs
go build -o bin/store-fs .

# smoke-test the protocol
mhl extension test .

# install into a project that also has a workflow + `extension store S { dir: "..." }`
mhl extension install .
```

Then in a `.mh` file under the serve directory:

```mhl
extension store S {
    dir: "/var/lib/mhl/state"
}
```

`mhl serve mcp --http <dir>` picks it up automatically — `run/*` checkpoints,
`run/*/owner`, and sessions all land in the extension. `--state-dir` is then
only a scratch path for the interpreter's own working files.

## Protocol

`initialize` handshake, `call` (`operation` + `named_args`), `shutdown` — the
newline-delimited JSON-RPC on stdin/stdout described in
[`../extension-protocol.md`](../extension-protocol.md). `main.go` is ~150 lines
and implementable in any language.
