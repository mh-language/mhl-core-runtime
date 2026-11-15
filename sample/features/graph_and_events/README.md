# graph_and_events

Two machine-readable interfaces of the `mhl` CLI for tools built on top of the language — editors,
visual designers, dashboards, CI: `mhl graph`, which turns a `.mh` file into its control-flow
graph, and `mhl run --format jsonl`, which streams a run as events. Neither is needed to write or
run `.mh`; the mhl IDE (`src/mhl-ide`) is one consumer of both.

- [order_triage.mh](order_triage.mh) — one workflow using every shape the graph draws:
  fall-through between steps, a `parallel` group, `goto match` (with a `_ -> fail(...)` arm), a
  reusable `route`, a guarded `pause(...)` gate, the `complete()` / `break` terminals, and a
  step calling a method of a declared `tool`. Its `test` block runs each path with
  `OrderTriage.run(...)`.

  ```
  mhl graph sample/features/graph_and_events/order_triage.mh
  ```
  prints one JSON object: `workflows[].nodes` (steps and parallel groups, each with the byte
  spans of its header, name, timeout clause and body, and its `callSites` — every
  `Receiver.method(...)` call it makes, with the call's text and span), `workflows[].edges` (`next` for
  fall-through, `goto`, `match`, `route`, `end`, `fail`, each with a `label` and the span of
  its target name), the `declarations` a step can call, and parse/lint `diagnostics`. A `tool`
  declaration also lists its `methods` — each with `args` (name, type, default), `returns`,
  `internal`, and the spans of its header and body (`bodyKind` `expr` or `block`) — and its
  `refs`, every `Pricing.` call site in the file, so an editor can rewrite a signature, a body
  or the tool's name in place. Pass
  `--stdin` to graph an unsaved buffer — the path then only names the file. A file that does
  not parse reports `"ok": false` and still exits 0.

  ```
  mhl run sample/features/graph_and_events/order_triage.mh --format jsonl --input kind=large
  ```
  prints one JSON event per line — `run_start`, a `step_start` / `step_end` pair per step
  (`status`: `ok`, `failed`, `goto`, `pause`, `break` or `complete`), a `log` per output
  line, and a final `result` carrying the same object `--format json` prints. Here the run
  ends paused at `Review`; continue it with
  `--resume --input kind=large --input approved=true`. Ctrl-C (SIGINT) cancels the run with a
  `"kind": "cancelled"` result and a checkpoint `--resume` continues from.
