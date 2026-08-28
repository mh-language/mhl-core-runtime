# http

The `http` native namespace is one op per verb — `get`, `post`, `put`, `patch`, `delete`,
`head`, `options` — plus `download`, all sharing a single optional-argument surface:
`headers`, `query`, `body`/`text`/`form` (JSON / raw / form-encoded, pick one), `timeout`,
`follow_redirects`, `raise_for_status`, `auth` (`{bearer}` or `{basic: {user, password}}`),
`tls` (`{cert, key, ca, insecure}` — PEM client certificate paths for mutual TLS, an extra
CA bundle, and the dev-only verification toggle), and `proxy` (an explicit proxy URL;
otherwise `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` are honoured). Every verb returns
`{status, body, headers, ok, json}`; `http.download` streams the response straight to a
file and returns `{status, path, bytes, ok, headers}`. A transport failure raises; a
non-2xx response does not unless `raise_for_status: true`.

A bearer token / basic password / `Authorization` header value passed to any `http.*`
call — and any `env("…_TOKEN" / "…_SECRET" / "…_API_KEY")` read — is registered as a secret
and shown as `[REDACTED]` in logs, error output, and persisted checkpoints.

These samples never reach the network — they bind every verb and option against a dead
port (an instant connection refusal) or hit an argument error that the runtime raises
before it dials, mirroring `sample/features/git`'s validation-only write-op examples.
Behavioural coverage (real requests, mTLS handshakes, proxying, downloads, response
parsing, secret redaction) lives in the Go tests at
`internal/features/nativeops/http_test.go`, `internal/features/auth/resolver_test.go`, and
`internal/cli/tool_test.go`.

- [http_verbs_share_one_option_surface.mh](http_verbs_share_one_option_surface.mh) — each
  of the seven verbs with a representative option (`query`, `body`, `text`, `form`,
  `timeout`, `headers`, `follow_redirects`), plus `auth: {bearer}` / `auth: {basic}`,
  `url` as the first positional argument, and `proxy:` / `http.download` taking the same
  options
- [http_argument_validation_raises_before_the_network.mh](http_argument_validation_raises_before_the_network.mh)
  — `body`/`text`/`form` are mutually exclusive, a `tls.cert` path that does not load
  raises, `url` is required, and `http.download` requires a destination path — all caught
  before any connection attempt
