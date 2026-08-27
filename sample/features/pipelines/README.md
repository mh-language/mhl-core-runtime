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
  pipeline-wide `spawn: { max_concurrency: N }`. Run with:
  ```
  mhl run sample/features/pipelines/concurrent_agents_pipeline_example.mh --input topic=caching
  ```
