# mcp

Declaring an `mcp_server` and calling it from `.mh` code with `<Server>.call("toolName", { ... })`,
`<Server>.list_tools()`, or `<Server>.discover()` — the operations an mcp_server exposes today.
All three dispatch to `internal/features/mcp`'s stateless JSON-RPC client: `stdio` spawns a fresh
process per call and speaks newline-delimited JSON-RPC over its stdin/stdout, `http` issues a
fresh POST per call with the declared headers (commonly a bearer token resolved via `env(...)`).
Credential resolution is fail-closed — a header referencing an unset env var aborts the call
rather than sending one with an empty value — and no session or connection is kept between calls
on either transport.

Per spec 2026-07-28 (the revision this client targets — `mcp.SpecVersion`), MCP has no
`initialize` handshake or protocol-level session: every request is self-describing instead. The
client sets `params._meta`'s `io.modelcontextprotocol/protocolVersion`, `clientCapabilities`, and
`clientInfo` (`{name: "mhl"}`) on every request, and on the `http` transport also sends the three
headers the transport spec requires (`MCP-Protocol-Version`, `Mcp-Method`, `Mcp-Name`),
Base64-sentinel-encoding a header value that isn't safe plain ASCII. A response whose `resultType`
is `"input_required"` (the Multi Round-Trip Requests pattern — the server wants elicitation/
sampling input) is rejected with a clear error, since this client can't gather and resubmit that
input.

- [stdio_mock_server_proves_tools_call_wiring.mh](stdio_mock_server_proves_tools_call_wiring.mh)
  — a mocked `stdio` server (a one-line shell script standing in for a real one) proves the
  `tools/call` request/response round trip deterministically, with no network involved
- [stdio_mock_server_proves_list_tools_wiring.mh](stdio_mock_server_proves_list_tools_wiring.mh)
  — the same, for `.list_tools()` (`tools/list`)
- [stdio_mock_server_proves_discover_wiring.mh](stdio_mock_server_proves_discover_wiring.mh) —
  the same, for `.discover()` (`server/discover`)
- [stdio_mock_server_rejects_input_required_result.mh](stdio_mock_server_rejects_input_required_result.mh)
  — a mocked `"input_required"` result proves `.call()` rejects it instead of returning it as if
  it were real tool data
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
