package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectRootWalksUpToLockFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".mhl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mhl", "extensions.lock"), []byte(`{"extensions":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "main.mh")

	got, ok := findProjectRoot(file)
	if !ok {
		t.Fatal("expected to find the project root")
	}
	if got != root {
		t.Fatalf("root = %q, want %q", got, root)
	}
}

func TestFindProjectRootNoLockFileFound(t *testing.T) {
	dir := t.TempDir()
	if _, ok := findProjectRoot(filepath.Join(dir, "main.mh")); ok {
		t.Fatal("expected no project root without an extensions.lock anywhere up the tree")
	}
}

func TestProjectExtensionSpecReadsMHManifest(t *testing.T) {
	dir := t.TempDir()
	extDir := filepath.Join(dir, ".mhl", "extensions", "dev.mhl.cache-repro")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `extensible cache {
    manifest: { id: "dev.mhl.cache-repro", api_version: "1", executable: "bin/cache-repro" }
    properties: { url: string }
    get(key: string): any
}
`
	if err := os.WriteFile(filepath.Join(extDir, "extension.mh"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, ".mhl", "extensions.lock")
	if err := os.WriteFile(lockPath, []byte(`{"extensions":{"dev.mhl.cache-repro":{"version":"","sha256":""}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	spec, ok := projectExtensionSpec(filepath.Join(dir, "main.mh"), "cache", "")
	if !ok {
		t.Fatal("expected to resolve the \"cache\" kind")
	}
	if len(spec.Properties) != 1 || spec.Properties[0].Name != "url" {
		t.Fatalf("unexpected properties: %+v", spec.Properties)
	}
	if len(spec.Methods) != 1 || spec.Methods[0].Name != "get" {
		t.Fatalf("unexpected methods: %+v", spec.Methods)
	}

	if _, ok := projectExtensionSpec(filepath.Join(dir, "main.mh"), "unknown", ""); ok {
		t.Fatal("expected no spec for a kind no locked extension declares")
	}
}

// writeLockedExtension vendors a single-file extension.mh manifest for id
// (serving kind, with one method) under dir/.mhl/extensions/<id>/.
func writeLockedExtension(t *testing.T, dir, id, kind, method string) {
	t.Helper()
	extDir := filepath.Join(dir, ".mhl", "extensions", id)
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "extensible " + kind + " {\n" +
		"    manifest: { id: \"" + id + "\", api_version: \"1\", executable: \"bin/x\" }\n" +
		"    " + method + "(key: string): any\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(extDir, "extension.mh"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectExtensionSpecPicksProviderAmongSameKind(t *testing.T) {
	dir := t.TempDir()
	writeLockedExtension(t, dir, "mhl.data.redis", "data", "incr")
	writeLockedExtension(t, dir, "mhl.data.postgres", "data", "query")
	lock := `{"extensions":{"mhl.data.redis":{"version":"","sha256":""},"mhl.data.postgres":{"version":"","sha256":""}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mhl", "extensions.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "main.mh")

	tests := []struct {
		provider   string
		wantOK     bool
		wantMethod string
	}{
		{"mhl.data.redis", true, "incr"},
		{"mhl.data.postgres", true, "query"},
		{"", false, ""},                 // ambiguous: two locked extensions serve "data"
		{"mhl.data.missing", false, ""}, // not locked
	}
	for _, tt := range tests {
		spec, ok := projectExtensionSpec(file, "data", tt.provider)
		if ok != tt.wantOK {
			t.Fatalf("provider %q: ok = %v, want %v", tt.provider, ok, tt.wantOK)
		}
		if ok && (len(spec.Methods) != 1 || spec.Methods[0].Name != tt.wantMethod) {
			t.Fatalf("provider %q: methods = %+v, want [%s]", tt.provider, spec.Methods, tt.wantMethod)
		}
	}
}
