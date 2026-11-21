// Package mhfile names the file extensions an mhl source file may carry.
//
// ".mh" is canonical — it is what `mhl init` and every scaffold writes — and
// ".mhl" is an accepted alias. Anything that discovers source files by
// scanning a directory (lint, test, serve, the LSP's sibling-file lookups)
// must go through Is rather than a hard-coded suffix, so the two stay
// interchangeable. A path named explicitly (`mhl run x.mhl`, an `import`)
// never needed an extension check at all.
package mhfile

import "path/filepath"

// Ext is the canonical source extension, used when mhl writes a new file.
const Ext = ".mh"

// Extensions lists every accepted source extension, canonical first.
var Extensions = []string{Ext, ".mhl"}

// Is reports whether name (a base name or a path) ends in one of
// Extensions. The match is case-sensitive, like the suffix checks it replaced.
func Is(name string) bool {
	ext := filepath.Ext(name)
	for _, e := range Extensions {
		if ext == e {
			return true
		}
	}
	return false
}
