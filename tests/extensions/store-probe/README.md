# store-probe — an instrumented `store` extension for stress tests

A drop-in for [`../store-fs`](../store-fs): same kind (`store`), same four
methods (`get` / `put` / `delete` / `list`), same on-disk layout (one JSON file
per key under `dir`, atomic rename) — so `mhl serve mcp --http` uses it for
durable run/session state exactly like the reference.

On top of that it takes **knobs from the declaration's own properties**, so a
test tunes behaviour without editing the manifest:

| property | effect |
|---|---|
| `dir` | storage root (persists across processes) |
| `log` | append one JSON line per handled message — `{"ev":"init\|call\|shutdown\|crash", "t":<unixNano>, "seq":N, "op":"put", "key":"…", "dur_us":…}` |
| `latency_ms` | `time.Sleep` that long inside every call |
| `crash_after` | `os.Exit(1)` after N calls — exercises the host's respawn / `maxRestarts` |
| `serial` | `true` = one call at a time (store-fs behaviour); default = a goroutine per call |

Used by the scenario suite in [`../tests_ext/`](../tests_ext/) to exercise:

- loading / lock allow-list / `env()` in props / process lifecycle;
- **concurrency** — a `parallel` group of `S.put` calls (host id-multiplexes,
  the extension handles them concurrently under one storage mutex), the
  serial-vs-concurrent wall-time difference, one process shared by many
  `extension store` declarations, restart on a mid-run crash;
- the `mhl serve mcp --http` path — `run/*` checkpoints / `owner` / sessions
  landing in the extension, restart-reclaim, concurrent `run/start`.

## Build

```sh
cd tests/extensions/store-probe
go build -o bin/store-probe .
mhl extension test .          # protocol smoke test
```

The scenario `run.sh` scripts build and `mhl extension install` it into a
scratch project on demand — nothing to install globally.
