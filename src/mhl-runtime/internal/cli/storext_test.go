package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

func parseDir(t *testing.T, dir string) []execsvc.File {
	t.Helper()
	files, err := execsvc.ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	return files
}

func TestScanStoreDecl(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "wf.mh"), []byte("pipeline P { step S { var x = 1 } }"), 0o644)
		_, ok, err := scanStoreDecl(dir, parseDir(t, dir))
		if err != nil || ok {
			t.Fatalf("no store decl: ok=%v err=%v", ok, err)
		}
	})

	t.Run("one with literal props", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "s.mh"),
			[]byte(`extension store Backend {`+"\n"+`  dir: "/var/lib/mhl"`+"\n"+`  region: "us-east-1"`+"\n}"), 0o644)
		decl, ok, err := scanStoreDecl(dir, parseDir(t, dir))
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if decl.Kind != "store" || decl.Name != "Backend" {
			t.Fatalf("decl = %+v", decl)
		}
		got := map[string]any{}
		for _, p := range decl.Props {
			got[p.Name] = p.Value
		}
		if got["dir"] != "/var/lib/mhl" || got["region"] != "us-east-1" {
			t.Errorf("props = %v", got)
		}
	})

	t.Run("two is an error", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "a.mh"), []byte(`extension store A { dir: "x" }`), 0o644)
		os.WriteFile(filepath.Join(dir, "b.mh"), []byte(`extension store B { dir: "y" }`), 0o644)
		if _, _, err := scanStoreDecl(dir, parseDir(t, dir)); err == nil || !strings.Contains(err.Error(), "more than one") {
			t.Fatalf("err = %v, want 'more than one'", err)
		}
	})
}

// A `store` decl with no installed extension is a clear startup error, not a
// silent fallback to disk.
func TestDiscoverStoreExtensionNotInstalled(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "s.mh"), []byte(`extension store S { dir: "x" }`), 0o644)
	_, _, err := discoverStoreExtension(dir, parseDir(t, dir), os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "no installed extension serves kind") {
		t.Fatalf("err = %v", err)
	}
}
