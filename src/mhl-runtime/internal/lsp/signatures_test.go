package lsp

import (
	"sort"
	"testing"
)

// TestSignatureCatalogueMatchesSymbolTables is the drift guard the
// signatures.go header promises: every method the LSP offers in completion
// must have a signature entry, and every signature entry must correspond to
// a real method — so adding/removing a runtime callable can't leave the two
// halves of this package inconsistent.
func TestSignatureCatalogueMatchesSymbolTables(t *testing.T) {
	// native ops: nativeSymbols <-> nativeSigs
	nativeExpected := map[string]bool{}
	for _, ns := range nativeSymbols {
		for _, m := range ns.Methods {
			key := ns.Name + "." + m
			nativeExpected[key] = true
			if _, ok := nativeSigs[key]; !ok {
				t.Errorf("nativeSigs is missing %q (it is in nativeSymbols)", key)
			}
		}
	}
	for key := range nativeSigs {
		if !nativeExpected[key] {
			t.Errorf("nativeSigs has %q, which is not in nativeSymbols", key)
		}
	}

	// value methods: stringMethods/arrayMethods/objectMethods resolve through
	// their own table plus the shared common table.
	checkValueKind(t, "string", stringMethods, stringMethodSigs)
	checkValueKind(t, "array", arrayMethods, arrayMethodSigs)
	checkValueKind(t, "object", objectMethods, objectMethodSigs)

	// mcp_server
	checkExact(t, "mcpServerMethodSigs", mcpServerMethods, mcpServerMethodSigs)

	// a2a_agent
	checkExact(t, "a2aAgentMethodSigs", a2aAgentMethods, a2aAgentMethodSigs)

	// memory: every method any backend type exposes must resolve, plus the
	// ephemeral mem's reset().
	memMethods := map[string]bool{"reset": true}
	for _, typ := range []string{"", "kv", "json", "append_log", "jsonl"} {
		for _, m := range memoryMethodsForType(typ) {
			memMethods[m] = true
		}
	}
	for m := range memMethods {
		if _, ok := memoryMethodSigs[m]; !ok {
			t.Errorf("memoryMethodSigs is missing %q", m)
		}
	}
	for m := range memoryMethodSigs {
		if !memMethods[m] {
			t.Errorf("memoryMethodSigs has %q, which no memory backend exposes", m)
		}
	}

	// agent
	checkExact(t, "agentMethodSigs", []string{"run"}, agentMethodSigs)

	// globals + assertions: pinned to the interpreter's own lists (eval.go
	// evalPostfix special-cases; test.go runAssertion).
	checkExact(t, "globalSigs", []string{
		"log", "fail", "env",
		"type_of", "is_string", "is_number", "is_bool", "is_array", "is_object", "is_null",
	}, globalSigs)
	checkExact(t, "assertionSigs", []string{
		"are_equal", "are_not_equal", "not_equal", "is_true", "is_false",
		"is_null", "not_null", "greater_than", "less_than",
		"greater_than_or_equal", "less_than_or_equal", "includes", "incomplete",
	}, assertionSigs)
}

func checkValueKind(t *testing.T, kind string, methods []string, own map[string]sig) {
	t.Helper()
	for _, m := range methods {
		if _, ok := own[m]; ok {
			continue
		}
		if _, ok := commonMethodSigs[m]; ok {
			continue
		}
		t.Errorf("%s method %q has no signature (not in its own table nor commonMethodSigs)", kind, m)
	}
	known := map[string]bool{}
	for _, m := range methods {
		known[m] = true
	}
	for m := range own {
		if !known[m] {
			t.Errorf("%sMethodSigs has %q, which is not in the %s method list", kind, m, kind)
		}
	}
}

func checkExact(t *testing.T, name string, want []string, got map[string]sig) {
	t.Helper()
	for _, m := range want {
		if _, ok := got[m]; !ok {
			t.Errorf("%s is missing %q", name, m)
		}
	}
	if len(got) != len(want) {
		var extra []string
		wantSet := map[string]bool{}
		for _, m := range want {
			wantSet[m] = true
		}
		for m := range got {
			if !wantSet[m] {
				extra = append(extra, m)
			}
		}
		sort.Strings(extra)
		if len(extra) > 0 {
			t.Errorf("%s has unexpected entries: %v", name, extra)
		}
	}
}

// TestEverySignatureHasParamsForItsPlaceholders sanity-checks each catalogue
// entry's shape: a non-empty Params list should line up with the Label
// actually taking arguments.
func TestSignatureLabelsNonEmpty(t *testing.T) {
	all := []map[string]sig{
		nativeSigs, commonMethodSigs, stringMethodSigs, arrayMethodSigs,
		objectMethodSigs, memoryMethodSigs, mcpServerMethodSigs,
		a2aAgentMethodSigs, agentMethodSigs, globalSigs, assertionSigs,
	}
	for _, table := range all {
		for name, s := range table {
			if s.Label == "" {
				t.Errorf("%q has an empty Label", name)
			}
		}
	}
}
