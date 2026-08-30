package external

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "extension.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestManifestMinimalDeclarationIsValid: routing is the only requirement —
// properties and methods are optional tooling metadata.
func TestManifestMinimalDeclarationIsValid(t *testing.T) {
	dir := t.TempDir()
	p := writeManifest(t, dir, `{
		"id": "com.acme.crm",
		"api_version": "1",
		"executable": "bin/crm",
		"declarations": [{ "kind": "crm" }]
	}`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("a kind-only declaration should be valid: %v", err)
	}
	if len(m.Declares) != 1 || m.Declares[0].Kind != "crm" {
		t.Fatalf("unexpected declares: %+v", m.Declares)
	}
	if len(m.Declares[0].Methods) != 0 {
		t.Fatalf("expected no methods, got %+v", m.Declares[0].Methods)
	}
}

func TestManifestDeclarationsFileSidecar(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "crm.d.json"), []byte(`[
		{ "kind": "crm", "methods": [{ "name": "lookup", "signature": "lookup(id: string) -> object" }] }
	]`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := writeManifest(t, dir, `{
		"id": "com.acme.crm",
		"api_version": "1",
		"executable": "bin/crm",
		"declarations_file": "crm.d.json"
	}`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Declares) != 1 || len(m.Declares[0].Methods) != 1 || m.Declares[0].Methods[0].Name != "lookup" {
		t.Fatalf("sidecar declarations not loaded: %+v", m.Declares)
	}
}

func TestManifestDeclarationsFileWrappedForm(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "d.json"), []byte(`{ "declarations": [{ "kind": "crm" }] }`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations_file": "d.json"
	}`)
	if _, err := LoadManifest(p); err != nil {
		t.Fatalf("wrapped { \"declarations\": [...] } form should load: %v", err)
	}
}

func TestManifestRejectsBothInlineAndFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "d.json"), []byte(`[{ "kind": "crm" }]`), 0o644)
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations": [{ "kind": "crm" }],
		"declarations_file": "d.json"
	}`)
	_, err := LoadManifest(p)
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("expected a both-forms error, got: %v", err)
	}
}

func TestManifestMissingSidecarIsAnError(t *testing.T) {
	dir := t.TempDir()
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations_file": "nope.json"
	}`)
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected an error for a missing declarations_file")
	}
}

func TestManifestRejectsDuplicateKind(t *testing.T) {
	dir := t.TempDir()
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations": [{ "kind": "crm" }, { "kind": "crm" }]
	}`)
	if _, err := LoadManifest(p); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("expected a duplicate-kind error, got: %v", err)
	}
}
