package ast

import "github.com/alecthomas/participle/v2/lexer"

// Extensible is a single-file, author-time declaration of an mhl external
// extension: `extensible <kind> { ... }` carries both the extension.json
// manifest (id, api_version, executable, permissions, ...) and the
// capability's declaration (properties/methods) that would otherwise live
// in a separate JSON manifest plus a declarations sidecar. Kind is a bare
// identifier, not a quoted string — the same shape an `extension <kind>
// <Name>` usage site already writes it in, e.g. `extension mcp GitHub`; a
// declaration keeps the two consistent rather than introducing a `kind
// "..."` clause of its own. It is read by internal/extension/external's
// manifest loader — never by the interpreter — so its Items give back plain
// data (a manifest object literal, a properties block, bare method
// signatures), not runtime behavior. A leading `/** ... **/` block comment
// and trailing `///...` line comments carry documentation; neither is part
// of this grammar (mhl has no block-comment syntax, and `///...` already
// lexes as an ordinary elided line comment — the third `/` is just more
// comment text to the lexer) — internal/extension/external recovers both
// with its own scan of the source text, keyed off each node's Pos.
//
//	/**
//	TTL-first key/value cache backed by Redis.
//	**/
//	extensible cache {
//	    manifest: {
//	        id: "dev.mhl.cache-redis",
//	        api_version: "1",
//	        executable: "bin/mhl-cache-redis"
//	    }
//	    properties: {
//	        url: string /// redis://[user:pass@]host:port/db
//	    }
//	    get(key: string) -> any /// The JSON-decoded value, or null.
//	}
type Extensible struct {
	Pos   lexer.Position
	Kind  string            `parser:"'extensible' @Ident '{'"`
	Items []*ExtensibleItem `parser:"@@* '}'"`
}

// ExtensibleItem is one entry of an Extensible body: the manifest object
// literal, a properties block, or one bare method signature. Exactly one
// field is non-nil.
type ExtensibleItem struct {
	Pos        lexer.Position
	Manifest   *Expr                 `parser:"( 'manifest' ':' @@"`
	Properties []*ExtensibleProperty `parser:"| 'properties' ':' '{' @@* '}'"`
	Method     *ExtensibleMethod     `parser:"| @@ )"`
}

// ExtensibleProperty is one `name: Type` entry of a properties block — a
// config field the extension's declaration accepts, with no default or
// value, just its shape.
type ExtensibleProperty struct {
	Pos  lexer.Position
	Name string    `parser:"@Ident ':'"`
	Type *TypeExpr `parser:"@@"`
}

// ExtensibleMethod is one bare `name(params) -> ReturnType` signature — an
// operation the bound capability exposes. It has no body: Extensible only
// declares the surface, an external process implements it.
type ExtensibleMethod struct {
	Pos     lexer.Position
	Name    string    `parser:"@Ident '('"`
	Params  []*Param  `parser:"( @@ ( ',' @@ )* )? ')'"`
	Returns *TypeExpr `parser:"'->' @@"`
}
