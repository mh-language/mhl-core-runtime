# a2a

Declaring an `extension a2a <Name> { ... }` and calling it from `.mh` code with `<Agent>.send("...")`,
`<Agent>.agent_card()`, `<Agent>.get_task(id)`, and `<Agent>.cancel(id)` — the operations an
an a2a extension exposes. All dispatch to `internal/features/a2a`'s stateless client: a fresh
JSON-RPC 2.0 POST per call (`message/send`, `tasks/get`, `tasks/cancel`), or a plain GET of
`<origin>/.well-known/agent-card.json` for `agent_card`. Credential resolution is fail-closed —
a header referencing an unset env var aborts the call — and no session is kept between calls. A
blocking `.send` that polls `tasks/get` to a terminal state issues several requests but carries
no state beyond the task id.

The client targets A2A revision 0.2.x (`a2a.ProtocolVersion`): method names `message/send` /
`tasks/get` / `tasks/cancel`, Agent Card at `/.well-known/agent-card.json`. `.send` sends the
string as one text part; the result is normalized to `{kind: "message", text, ...}` or
`{kind: "task", task_id, state, text, artifacts, ...}`, where `text` concatenates every text
part for convenience. A task that stops in `input-required` / `auth-required` is rejected with a
clear error, since this client can't gather and resubmit that input. `message/stream` (SSE) and
push notifications are not implemented.

- [a2a_send_calls_the_real_api.mh](a2a_send_calls_the_real_api.mh) — `.send(...)` and
  `.agent_card()` against a real A2A agent, gated behind `A2A_TEST_URL` (and `A2A_TOKEN` for
  auth) being set

**No network-free sample here.** Proving the wiring deterministically needs a controllable HTTP
server that returns a Task, then serves `tasks/get` polls, then an Agent Card at the well-known
path — `.mh` has no way to stand up an HTTP server itself (only `stdio` mocks via a shell
script, as the `mcp/` samples do). It's covered instead by `TestA2AAgentSendPollsTaskToCompletion`
and friends in `src/mhl-runtime/internal/cli/a2a_test.go`, plus
`src/mhl-runtime/internal/features/a2a/client_test.go`, using a real `httptest.Server`.
