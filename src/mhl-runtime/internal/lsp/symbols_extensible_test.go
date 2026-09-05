package lsp

import (
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// An `extensible` declaration shows up in the outline like any other
// top-level declaration, with its bare method signatures as members.
func TestSymbolsFromProgramIncludesExtensible(t *testing.T) {
	prog, err := parser.Parse(`
extensible cache {
    manifest: { id: "x", api_version: "1", executable: "bin/x" }
    properties: { url: string }
    get(key: string) -> any
    set(key: string, value: any) -> void
}
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	syms := symbolsFromProgram(prog)
	var found *symbol
	for i := range syms {
		if syms[i].Name == "cache" {
			found = &syms[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a cache symbol, got %+v", syms)
	}
	if found.Kind != symExtensible {
		t.Fatalf("expected symExtensible, got %v", found.Kind)
	}
	if len(found.Methods) != 2 || found.Methods[0] != "get" || found.Methods[1] != "set" {
		t.Fatalf("unexpected methods: %+v", found.Methods)
	}
}
