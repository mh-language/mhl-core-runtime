package external

import (
	"os"
	"path/filepath"
	"testing"
)

const aliasManifest = `extensible cache {
    manifest: {
        id: "dev.mhl.cache-alias",
        version: "0.1.0",
        api_version: "1",
        executable: "bin/mhl-cache-alias",
        permissions: { network: [], secrets: [] }
    }

    get(key: string): any
}
`

// An extension.mhl is found and loaded exactly like an extension.mh: ".mhl"
// is an alias of the source extension.
func TestFindManifestFileAcceptsMhlAlias(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "extension.mhl")
	if err := os.WriteFile(want, []byte(aliasManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := FindManifestFile(dir)
	if !ok || got != want {
		t.Fatalf("FindManifestFile = %q, %v; want %q, true", got, ok, want)
	}
	m, err := LoadManifest(got)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.ID != "dev.mhl.cache-alias" || len(m.Declares) != 1 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

// With both present, the canonical extension.mh wins.
func TestFindManifestFilePrefersCanonicalMh(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"extension.mh", "extension.mhl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(aliasManifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := FindManifestFile(dir); filepath.Base(got) != "extension.mh" {
		t.Fatalf("FindManifestFile = %q, want extension.mh", got)
	}
}

// A declarations sidecar named *.mhl routes through the mhl parser too.
func TestDeclarationsFileAcceptsMhlAlias(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "declarations.mhl")
	if err := os.WriteFile(p, []byte(`[{ kind: "crm" }]`), 0o644); err != nil {
		t.Fatal(err)
	}
	specs, err := loadDeclarationsFile(p)
	if err != nil {
		t.Fatalf("loadDeclarationsFile: %v", err)
	}
	if len(specs) != 1 || specs[0].Kind != "crm" {
		t.Fatalf("unexpected specs: %+v", specs)
	}
}
