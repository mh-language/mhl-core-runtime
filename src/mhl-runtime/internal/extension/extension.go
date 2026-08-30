// Package extension defines the in-process contract between the mhl runtime
// core and a capability provider (an "extension"). MCP and A2A are migrating
// to become the first two implementations of this contract; see
// docs/plano-extensoes-mcp-a2a.md.
//
// Scope: every type an extension exchanges with the host is a plain,
// JSON-serialisable DTO — json tags, primitives, map[string]any, slices. The
// contract deliberately exposes no *ast.X, no interpreter internals, and no
// context.Context inside a struct (context travels as a Call parameter). That
// keeps the future out-of-process wire protocol a straight projection of
// these same structs rather than a redesign. The package lives under
// internal/ on purpose: it is promoted to pkg/ only once that wire protocol
// is frozen.
package extension

import "context"

// Extension is a registered capability provider. One Extension may back
// several declaration kinds (an "mcp" extension backs the `mcp` kind). It is
// stateless configuration plus a factory: per-declaration state lives on the
// Instance returned by Bind.
type Extension interface {
	// ID is a stable, globally unique identifier, e.g. "mhl.mcp".
	ID() string
	// Version is the extension's own semantic version, surfaced in
	// diagnostics and (later) the wire handshake.
	Version() string
	// Declarations lists the declaration kinds this extension serves and
	// the properties each accepts. Consumed by lint and the LSP.
	Declarations() []DeclarationSpec
	// Validate statically checks one declaration's properties. It must not
	// perform I/O or network calls. A nil/empty slice means "no problems".
	Validate(Declaration) []Diagnostic
	// Bind resolves one declaration against a HostContext and returns a
	// live Instance. The registry calls it at most once per
	// (execution, declaration) and caches the result.
	Bind(Declaration, HostContext) (Instance, error)
}

// Instance is one bound declaration, ready to serve calls.
type Instance interface {
	// Methods lists the callable operations this instance exposes.
	Methods() []MethodSpec
	// Call invokes one method by name. ctx carries cancellation and any
	// deadline; req carries the arguments and the originating declaration.
	Call(ctx context.Context, req CallRequest) (Value, error)
}
