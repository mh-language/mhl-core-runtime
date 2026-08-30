package lsp

import (
	"github.com/mh-language/mhl-core-runtime/internal/extension"

	_ "github.com/mh-language/mhl-core-runtime/internal/extbuiltin" // populate extension.Builtins()
)

// This file makes the LSP's knowledge of `extension` declarations
// metadata-driven: property lists, dot-callable method names, and signature
// help all come from the registered extension adapters' DeclarationSpec /
// MethodSpec rather than from tables hand-copied out of the interpreter. A
// new built-in extension kind (registered in internal/extbuiltin) is picked
// up here automatically.

// extensionMethodNames returns the dot-callable method names a built-in
// extension kind ("mcp", "a2a", ...) exposes, or nil for an unknown kind.
func extensionMethodNames(kind string) []string {
	spec, ok := extension.BuiltinSpec(kind)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(spec.Methods))
	for _, m := range spec.Methods {
		out = append(out, m.Name)
	}
	return out
}

// extensionPropertyItems returns the property-name completions valid directly
// inside a declaration body of the given kind.
func extensionPropertyItems(kind string) []completionItem {
	spec, ok := extension.BuiltinSpec(kind)
	if !ok {
		return nil
	}
	items := make([]completionItem, 0, len(spec.Properties))
	for _, p := range spec.Properties {
		detail := p.Documentation
		if p.Type != "" {
			detail = p.Type
			if p.Documentation != "" {
				detail += " — " + p.Documentation
			}
		}
		items = append(items, propertyItem(p.Name, detail))
	}
	return items
}

// extensionMethodSigs builds the signature-help table for a built-in
// extension kind from its MethodSpec entries.
func extensionMethodSigs(kind string) map[string]sig {
	spec, ok := extension.BuiltinSpec(kind)
	if !ok {
		return map[string]sig{}
	}
	out := make(map[string]sig, len(spec.Methods))
	for _, m := range spec.Methods {
		params := make([]string, 0, len(m.Params))
		for _, p := range m.Params {
			params = append(params, p.Name)
		}
		label := m.Signature
		if label == "" {
			label = m.Name + "(...)"
		}
		out[m.Name] = sig{Label: label, Params: params, Doc: m.Documentation}
	}
	return out
}
