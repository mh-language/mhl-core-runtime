package lsp

import (
	"github.com/mh-language/mhl-core-runtime/internal/extension"

	_ "github.com/mh-language/mhl-core-runtime/internal/extbuiltin" // populate extension.Builtins()
)

// This file makes the LSP's knowledge of `extension` declarations
// metadata-driven: property lists, dot-callable method names, and signature
// help all come from a DeclarationSpec / MethodSpec rather than from tables
// hand-copied out of the interpreter. A new built-in extension kind
// (registered in internal/extbuiltin) is picked up here automatically; an
// unrecognized kind falls back to path's project's locked external
// extensions (projectext.go) — the ones `mhl extension install` vendored —
// so a custom kind (e.g. a `cache` extension backed by mhl-cache-redis) gets
// the same completion/signature-help support a built-in one does. path may
// be "" (the three package-level *Sigs/*Methods vars below, always for a
// built-in kind), in which case the project fallback is skipped.

// extensionSpec resolves kind to its DeclarationSpec: a built-in first
// (internal/extbuiltin's registered adapters), else path's project's locked
// external extensions. ok is false when neither knows kind.
func extensionSpec(path, kind string) (extension.DeclarationSpec, bool) {
	if spec, ok := extension.BuiltinSpec(kind); ok {
		return spec, true
	}
	if path == "" {
		return extension.DeclarationSpec{}, false
	}
	return projectExtensionSpec(path, kind)
}

// extensionMethodNames returns the dot-callable method names extension kind
// exposes, or nil for an unknown kind.
func extensionMethodNames(path, kind string) []string {
	spec, ok := extensionSpec(path, kind)
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
func extensionPropertyItems(path, kind string) []completionItem {
	spec, ok := extensionSpec(path, kind)
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

// extensionMethodSigs builds the signature-help table for an extension kind
// from its MethodSpec entries.
func extensionMethodSigs(path, kind string) map[string]sig {
	spec, ok := extensionSpec(path, kind)
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
