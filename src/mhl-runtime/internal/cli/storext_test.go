package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
	"github.com/mh-language/mhl-core-runtime/internal/extension"
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

	t.Run("state kind", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "s.mh"), []byte(`extension state S from "mhl.state.sqlite" { path: "x.db" }`), 0o644)
		decl, ok, err := scanStoreDecl(dir, parseDir(t, dir))
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if decl.Kind != "state" || decl.Name != "S" || decl.Provider != "mhl.state.sqlite" {
			t.Fatalf("decl = %+v", decl)
		}
	})

	t.Run("state and legacy store together is an error", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "a.mh"), []byte(`extension state A { dir: "x" }`), 0o644)
		os.WriteFile(filepath.Join(dir, "b.mh"), []byte(`extension store B { dir: "y" }`), 0o644)
		if _, _, err := scanStoreDecl(dir, parseDir(t, dir)); err == nil || !strings.Contains(err.Error(), "more than one") {
			t.Fatalf("err = %v, want 'more than one'", err)
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

// stateExt is a stand-in installed extension serving one kind.
type stateExt struct{ id, kind string }

func (e *stateExt) ID() string      { return e.id }
func (e *stateExt) Version() string { return "0.0.1" }
func (e *stateExt) Declarations() []extension.DeclarationSpec {
	return []extension.DeclarationSpec{{Kind: e.kind}}
}
func (e *stateExt) Validate(extension.Declaration) []extension.Diagnostic { return nil }
func (e *stateExt) Bind(extension.Declaration, extension.HostContext) (extension.Instance, error) {
	return nil, nil
}

// `state` is the canonical durable-state kind and `store` its legacy name;
// either declaration finds a package serving either name, and the resolved
// declaration carries the kind the package actually serves.
func TestLookupStateExtensionAcceptsBothKindNames(t *testing.T) {
	tests := []struct {
		name     string
		exts     []extension.Extension
		decl     extension.Declaration
		wantID   string
		wantKind string
		wantCode string
	}{
		{name: "state decl, state package", exts: []extension.Extension{&stateExt{"mhl.state.sqlite", "state"}},
			decl: extension.Declaration{Kind: "state"}, wantID: "mhl.state.sqlite", wantKind: "state"},
		{name: "state decl, legacy store package", exts: []extension.Extension{&stateExt{"old.store", "store"}},
			decl: extension.Declaration{Kind: "state"}, wantID: "old.store", wantKind: "store"},
		{name: "legacy store decl, state package", exts: []extension.Extension{&stateExt{"mhl.state.sqlite", "state"}},
			decl: extension.Declaration{Kind: "store"}, wantID: "mhl.state.sqlite", wantKind: "state"},
		{name: "from picks among state packages", exts: []extension.Extension{&stateExt{"mhl.state.sqlite", "state"}, &stateExt{"mhl.state.redis", "state"}},
			decl: extension.Declaration{Kind: "state", Provider: "mhl.state.redis"}, wantID: "mhl.state.redis", wantKind: "state"},
		{name: "from names a legacy store package", exts: []extension.Extension{&stateExt{"mhl.state.sqlite", "state"}, &stateExt{"old.store", "store"}},
			decl: extension.Declaration{Kind: "state", Provider: "old.store"}, wantID: "old.store", wantKind: "store"},
		{name: "ambiguous without from", exts: []extension.Extension{&stateExt{"mhl.state.sqlite", "state"}, &stateExt{"mhl.state.redis", "state"}},
			decl: extension.Declaration{Kind: "state"}, wantCode: "ambiguous-extension-kind"},
		{name: "nothing installed", decl: extension.Declaration{Kind: "state"}, wantCode: "unknown-extension-kind"},
		{name: "unknown provider", exts: []extension.Extension{&stateExt{"mhl.state.sqlite", "state"}},
			decl: extension.Declaration{Kind: "state", Provider: "mhl.state.missing"}, wantCode: "unknown-extension-provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := extension.NewRegistry(extension.NopHost{})
			for _, e := range tt.exts {
				if err := reg.TryRegister(e); err != nil {
					t.Fatal(err)
				}
			}
			ext, decl, err := lookupStateExtension(reg, tt.decl)
			if tt.wantCode != "" {
				var le *extension.LookupError
				if !errors.As(err, &le) || le.Code != tt.wantCode {
					t.Fatalf("err = %v, want code %q", err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if ext.ID() != tt.wantID || decl.Kind != tt.wantKind {
				t.Fatalf("got (%s, kind %q), want (%s, kind %q)", ext.ID(), decl.Kind, tt.wantID, tt.wantKind)
			}
		})
	}
}
