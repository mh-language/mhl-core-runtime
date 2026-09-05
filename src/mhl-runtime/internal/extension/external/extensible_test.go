package external

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExtensible(t *testing.T, dir, source string) string {
	t.Helper()
	p := filepath.Join(dir, "extension.mh")
	if err := os.WriteFile(p, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestExtensibleLoadsManifestAndDeclarations covers the full single-file
// shape: a leading `/** ... **/` kind doc, a manifest object, a properties
// block, and bare method signatures with inline `///` doc-comments.
func TestExtensibleLoadsManifestAndDeclarations(t *testing.T) {
	dir := t.TempDir()
	p := writeExtensible(t, dir, `/**
TTL-first key/value cache backed by Redis.
**/
extensible cache {
    manifest: {
        id: "dev.mhl.cache-redis",
        version: "0.1.0",
        api_version: "1",
        executable: "bin/mhl-cache-redis",
        permissions: {
            network: ["*"],
            secrets: []
        }
    }

    properties: {
        url: string /// redis://[user:pass@]host:port/db
        db: number /// Logical database index. Default 0.
    }

    get(key: string) -> any /// The JSON-decoded value, or null when the key is absent.
    delete(key: string) -> void
}
`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.ID != "dev.mhl.cache-redis" || m.Version != "0.1.0" || m.APIVersion != "1" || m.Executable != "bin/mhl-cache-redis" {
		t.Fatalf("unexpected manifest fields: %+v", m)
	}
	if len(m.Perms.Network) != 1 || m.Perms.Network[0] != "*" {
		t.Fatalf("permissions not applied: %+v", m.Perms)
	}
	if len(m.Declares) != 1 {
		t.Fatalf("expected 1 declaration kind, got %d", len(m.Declares))
	}
	d := m.Declares[0]
	if d.Kind != "cache" {
		t.Fatalf("kind = %q, want %q", d.Kind, "cache")
	}
	if !strings.Contains(d.Documentation, "TTL-first key/value cache backed by Redis.") {
		t.Fatalf("kind documentation not captured: %q", d.Documentation)
	}
	if len(d.Properties) != 2 || d.Properties[0].Name != "url" || d.Properties[0].Type != "string" {
		t.Fatalf("unexpected properties: %+v", d.Properties)
	}
	if d.Properties[0].Documentation != "redis://[user:pass@]host:port/db" {
		t.Fatalf("property doc = %q", d.Properties[0].Documentation)
	}
	if d.Properties[1].Documentation == "" {
		t.Fatalf("second property should have a doc-comment")
	}
	if len(d.Methods) != 2 {
		t.Fatalf("unexpected methods: %+v", d.Methods)
	}
	get := d.Methods[0]
	if get.Name != "get" || get.Signature != "get(key: string) -> any" {
		t.Fatalf("unexpected get() method: %+v", get)
	}
	if get.Documentation != "The JSON-decoded value, or null when the key is absent." {
		t.Fatalf("get() documentation = %q", get.Documentation)
	}
	if del := d.Methods[1]; del.Documentation != "" {
		t.Fatalf("delete() should have no doc-comment, got %q", del.Documentation)
	}
}

// TestExtensiblePlainCommentIsNotDocumentation confirms an ordinary `//`
// comment (no third slash) is left as just a comment — parsed fine, but not
// captured as Documentation — so authors aren't surprised by a stray `//`
// note quietly becoming user-facing doc text.
func TestExtensiblePlainCommentIsNotDocumentation(t *testing.T) {
	dir := t.TempDir()
	p := writeExtensible(t, dir, `extensible cache {
    manifest: { id: "x", api_version: "1", executable: "bin/x" }
    properties: {
        url: string // just a regular comment, not documentation
    }
    get(key: string) -> any // same here
}
`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	d := m.Declares[0]
	if d.Properties[0].Documentation != "" {
		t.Fatalf("expected no documentation from a plain // comment, got %q", d.Properties[0].Documentation)
	}
	if d.Methods[0].Documentation != "" {
		t.Fatalf("expected no documentation from a plain // comment, got %q", d.Methods[0].Documentation)
	}
}

// TestExtensibleMissingManifestIsAnError mirrors the JSON form's "missing
// executable/id" validation: a kind with no manifest block at all is
// rejected before ever reaching Manifest.validate().
func TestExtensibleMissingManifestIsAnError(t *testing.T) {
	dir := t.TempDir()
	p := writeExtensible(t, dir, `extensible cache {
    get(key: string) -> any
}
`)
	if _, err := LoadManifest(p); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("expected a missing-manifest error, got: %v", err)
	}
}

// TestExtensibleRejectsDuplicateManifest guards against a copy-paste error
// naming two manifest blocks in one file.
func TestExtensibleRejectsDuplicateManifest(t *testing.T) {
	dir := t.TempDir()
	p := writeExtensible(t, dir, `extensible cache {
    manifest: { id: "x", api_version: "1", executable: "bin/x" }
    manifest: { id: "y", api_version: "1", executable: "bin/y" }
}
`)
	if _, err := LoadManifest(p); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("expected a duplicate-manifest error, got: %v", err)
	}
}

// TestExtensibleValidatesLikeJSONManifest confirms the shared
// Manifest.validate() still runs — an unsupported api_version is rejected
// exactly as it would be for extension.json.
func TestExtensibleValidatesLikeJSONManifest(t *testing.T) {
	dir := t.TempDir()
	p := writeExtensible(t, dir, `extensible cache {
    manifest: { id: "x", api_version: "999", executable: "bin/x" }
}
`)
	if _, err := LoadManifest(p); err == nil || !strings.Contains(err.Error(), "api_version") {
		t.Fatalf("expected an api_version error, got: %v", err)
	}
}

// TestExtensiblePropertyAndMethodTypesRenderArraysAndShapes exercises a
// property/param type beyond a bare identifier, confirming
// loadExtensibleManifest reuses ast.TypeExpr.String() rather than
// re-implementing type rendering.
func TestExtensiblePropertyAndMethodTypesRenderArraysAndShapes(t *testing.T) {
	dir := t.TempDir()
	p := writeExtensible(t, dir, `extensible crm {
    manifest: { id: "x", api_version: "1", executable: "bin/x" }
    properties: {
        tags: string[]
    }
    lookup(ids: string[]) -> string[]
}
`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	d := m.Declares[0]
	if d.Properties[0].Type != "string[]" {
		t.Fatalf("property type = %q, want %q", d.Properties[0].Type, "string[]")
	}
	if d.Methods[0].Signature != "lookup(ids: string[]) -> string[]" {
		t.Fatalf("method signature = %q", d.Methods[0].Signature)
	}
}
