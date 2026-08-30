# mcp

Declaring an `extension mcp <Name> { ... }` and calling it from `.mh`
code with `<Server>.call("toolName", { ... })`, `<Server>.list_tools()`, or `<Server>.discover()`
— the operations it exposes.
All three dispatch to `internal/features/mcp`'s stateless JSON-RPC client: `stdio` spawns a fresh
process per call and speaks newline-delimited JSON-RPC over its stdin/stdout, `http` issues a
fresh POST per call with the declared headers (commonly a bearer token resolved via `env(...)`).
Credential resolution is fail-closed — a header referencing an unset env var aborts the call
rather than sending one with an empty value — and no session or connection is kept between calls
on either transport.

By default (`protocol: "auto"`) the client first tries the stateless `2026-07-28` form —
`mcp.SpecVersion`, no `initialize` handshake, every request self-describing via `params._meta`'s
`io.modelcontextprotocol/protocolVersion`, `clientCapabilities`, and `clientInfo` (`{name: "mhl", version}`),
and on `http` the three transport headers (`MCP-Protocol-Version`, `Mcp-Method`, `Mcp-Name`).
If the server rejects that with a protocol-incompatibility error (JSON-RPC `-32602`/`-32600`, or
HTTP `400`), the client automatically falls back to the standard `initialize` /
`notifications/initialized` handshake used by MCP revisions `2025-11-25` and `2025-06-18`
(dropping `_meta`, threading any `Mcp-Session-Id`). `protocol:` can pin any one of `"auto"`,
`"2026-07-28"`, `"2025-11-25"`, `"2025-06-18"`. A response whose `resultType` is
`"input_required"` (the Multi Round-Trip Requests pattern — the server wants elicitation/sampling
input) is rejected with a clear error, since this client can't gather and resubmit that input.

- [stdio_mock_server_proves_tools_call_wiring.mh](stdio_mock_server_proves_tools_call_wiring.mh)
  — a mocked `stdio` server (a one-line shell script standing in for a real one) proves the
  `tools/call` request/response round trip deterministically, with no network involved
- [stdio_mock_server_proves_list_tools_wiring.mh](stdio_mock_server_proves_list_tools_wiring.mh)
  — the same, for `.list_tools()` (`tools/list`)
- [extension_keyword_resolves_through_the_adapter.mh](extension_keyword_resolves_through_the_adapter.mh)
  — the generic `extension mcp <Name> { ... }` spelling dispatches `.list_tools()` through the
  very same in-process MCP adapter
- [stdio_mock_server_proves_discover_wiring.mh](stdio_mock_server_proves_discover_wiring.mh) —
  the same, for `.discover()` (`server/discover`)
- [stdio_mock_server_rejects_input_required_result.mh](stdio_mock_server_rejects_input_required_result.mh)
  — a mocked `"input_required"` result proves `.call()` rejects it instead of returning it as if
  it were real tool data
- [stdio_mock_server_negotiates_handshake_protocol.mh](stdio_mock_server_negotiates_handshake_protocol.mh)
  — a mocked *handshake* server (`initialize` → `notifications/initialized` → `tools/call`, no
  `_meta`) proves `protocol: "2025-11-25"` drives that lifecycle, and that `.discover()` maps to
  the `initialize` result when `server/discover` isn't available
- [github_mcp_calls_the_real_api.mh](github_mcp_calls_the_real_api.mh) — the real GitHub remote
  MCP server (`https://api.githubcopilot.com/mcp/`) over `http`, gated behind `GITHUB_TOKEN`
  being set to a real personal access token

**`x-mcp-header` has no sample here.** It's scoped to the `http` transport only (per spec, `stdio`
clients may ignore it), and demonstrating it needs a controllable HTTP server that can return a
`tools/list` schema with an `x-mcp-header` annotation and then inspect what headers the following
`tools/call` actually carried — `.mh` has no way to stand up an HTTP server itself (only `stdio`
mocks via a shell script, the pattern the samples above use). It's covered instead by
`TestMCPServerCallMirrorsXMCPHeaderAnnotatedArguments` in
`src/mhl-runtime/internal/cli/mcp_test.go`, using a real `httptest.Server`.
