package extension

import "net/http"

// HostContext is the capability surface the core hands an extension at Bind
// time. It is an interface, not a struct, so the in-process implementation
// (internal/engine/interpreter) and the future out-of-process broker are
// interchangeable without touching any extension. An extension holds the
// HostContext it is given and uses it for the lifetime of its Instance.
//
// The surface is deliberately narrow: an extension gets exactly the
// capabilities it needs, never raw OS access. Keeping it dependency-free here
// is what lets lint and the LSP import this package without pulling in the
// engine or the feature layer.
type HostContext interface {
	// ResolveSecret resolves a credential reference (e.g. `env("GITHUB_TOKEN")`)
	// to its value. It is fail-closed: an unresolvable reference returns an
	// error and the caller must abort rather than proceed with an empty
	// credential. Resolved values are registered with the host's redaction
	// machinery.
	ResolveSecret(ref string) (string, error)
	// HTTPClient returns an *http.Client whose transport is shared across the
	// whole execution for connection pooling. Extensions must not build their
	// own transports.
	HTTPClient() *http.Client
	// Logf writes a diagnostic line on the host's behalf, after redaction.
	Logf(format string, args ...any)
	// Redact replaces every known secret substring in s with a stable mask.
	// The host applies it to anything an extension emits that reaches a
	// user-visible surface — an error message, captured stderr — so a
	// credential the extension resolved cannot leak back out through them.
	Redact(s string) string
}

// NopHost is a HostContext that resolves no secrets, shares no configured
// transport, and swallows logs. It is for tests and for call paths (lint, the
// LSP) that only need the registry's metadata, never a live Bind.
type NopHost struct{}

func (NopHost) ResolveSecret(string) (string, error) { return "", nil }
func (NopHost) HTTPClient() *http.Client             { return http.DefaultClient }
func (NopHost) Logf(string, ...any)                  {}
func (NopHost) Redact(s string) string               { return s }
