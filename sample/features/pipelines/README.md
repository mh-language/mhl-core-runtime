# pipelines

Declaring a `pipeline` and wiring it to other features. Each example is run with `mhl run`,
not `mhl test` — pipelines aren't assertions, they're programs.

- [agent_pipeline_example.mh](agent_pipeline_example.mh) — one `agent`, one `pipeline` with a
  single `step` that interpolates the input and calls the agent. Run with:
  ```
  mhl run sample/features/pipelines/agent_pipeline_example.mh --input name=World
  ```
- [loop_poll_pipeline_example.mh](loop_poll_pipeline_example.mh) — a `loop pipeline` that
  repeats a `step` until a `memory`-backed `stop_when` condition is met, or a
  `max_iterations` ceiling is hit. Run with:
  ```
  mhl run sample/features/pipelines/loop_poll_pipeline_example.mh
  ```
- [concurrent_agents_pipeline_example.mh](concurrent_agents_pipeline_example.mh) — `spawn`
  starts an agent call in the background and binds a task handle; `wait` joins them
  (`wait a, b`, `wait any a, b`, `wait 2 of a, b, c`, `on_error: "collect"`), bounded by a
  pipeline-wide `spawn: { max_concurrency: N }`. The fan-out form
  `spawn xs = Agent.run(...) for item in <array>` starts one background call per element
  (each with a distinct prompt) and binds `xs` to an array of handles that `wait xs` joins
  as a group. Run with:
  ```
  mhl run sample/features/pipelines/concurrent_agents_pipeline_example.mh --input topic=caching
  ```
- [parallel_steps_pipeline_example.mh](parallel_steps_pipeline_example.mh) — a
  `parallel <Name> { step … step … }` group runs whole steps concurrently and joins them all
  before the pipeline advances (a barrier); each branch's var writes are merged back on join,
  with a different-value collision failing the run. The step-level counterpart of `spawn`.
  Run with:
  ```
  mhl run sample/features/pipelines/parallel_steps_pipeline_example.mh --input topic=caching
  ```
- [context_pipeline_example.mh](context_pipeline_example.mh) — a `context: { source, require }`
  block exposes the read-only `context` accessor (`context.session_id`, `context.started_at`,
  `context.resumed`, `context.vars`) to every step, carrying the previous completed run's
  variable state forward. Each run gets its own `.mhl/state/<session-id>/` directory;
  `--session <id>` pins it, a bare `--resume` follows the `.latest` pointer. Run it twice:
  ```
  mhl run sample/features/pipelines/context_pipeline_example.mh
  mhl run sample/features/pipelines/context_pipeline_example.mh
  ```
- [session_scoped_memory_pipeline_example.mh](session_scoped_memory_pipeline_example.mh) — a
  `memory` block's `path:` is interpolated for `${...}` spans (like an agent's `log:` path), so
  `path: ".mhl/session/state.${context.session_id}.json"` gives every run its own store. Run it
  twice to see a file pair per session id:
  ```
  mhl run sample/features/pipelines/session_scoped_memory_pipeline_example.mh
  mhl run sample/features/pipelines/session_scoped_memory_pipeline_example.mh
  ```
