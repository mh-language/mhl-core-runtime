package extension

import (
	"fmt"
	"sort"
	"sync"
)

// builtins is the set of extension implementations compiled into the binary.
// It is populated by RegisterBuiltin from package init functions — the MCP and
// A2A adapters register here (via internal/extbuiltin), and tests register
// fakes the same way. Every consumer that needs the built-in set reads it
// from here: the interpreter seeds each per-execution Registry from it, and
// lint / the LSP read BuiltinSpecs for metadata-driven diagnostics and
// completion.
//
// This lives in internal/extension (a dependency-free package) rather than in
// the interpreter so lint and the LSP can reach it without importing the
// engine.
var (
	builtinMu sync.RWMutex
	builtins  []Extension
)

// RegisterBuiltin adds ext to the built-in set. Call it from an init
// function. Registering the same ID twice panics — the built-in set is fixed
// at build time.
func RegisterBuiltin(ext Extension) {
	builtinMu.Lock()
	defer builtinMu.Unlock()
	for _, existing := range builtins {
		if existing.ID() == ext.ID() {
			panic(fmt.Sprintf("extension: builtin %q registered twice", ext.ID()))
		}
	}
	builtins = append(builtins, ext)
}

// Builtins returns the registered built-in extensions in registration order.
func Builtins() []Extension {
	builtinMu.RLock()
	defer builtinMu.RUnlock()
	out := make([]Extension, len(builtins))
	copy(out, builtins)
	return out
}

// NewBuiltinRegistry builds a Registry seeded with every built-in extension,
// binding instances against host.
func NewBuiltinRegistry(host HostContext) *Registry {
	reg := NewRegistry(host)
	for _, ext := range Builtins() {
		reg.Register(ext)
	}
	return reg
}

// BuiltinSpecs returns every declaration spec across all built-in extensions,
// sorted by kind. Consumed by lint and the LSP so neither has to know which
// extension owns which kind.
func BuiltinSpecs() []DeclarationSpec {
	var out []DeclarationSpec
	for _, ext := range Builtins() {
		out = append(out, ext.Declarations()...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// BuiltinSpec returns the declaration spec for one kind across the built-in
// set, and whether a built-in serves it.
func BuiltinSpec(kind string) (DeclarationSpec, bool) {
	for _, spec := range BuiltinSpecs() {
		if spec.Kind == kind {
			return spec, true
		}
	}
	return DeclarationSpec{}, false
}
