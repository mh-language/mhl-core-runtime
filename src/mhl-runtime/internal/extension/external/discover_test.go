package external

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// scaffold writes a project dir containing .mhl/extensions/<id>/extension.json
// (pointing the executable at this test binary as the fake extension) and a
// .mhl/extensions.lock. lockSHA overrides the pinned hash; "" means "use the
// real hash of the test binary".
func scaffold(t *testing.T, id, lockSHA string, includeInLock bool) string {
	t.Helper()
	root := t.TempDir()
	extDir := filepath.Join(root, ".mhl", "extensions", id)
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}

	man := Manifest{
		ID: id, Version: "1.0.0", APIVersion: APIVersion,
		Executable: testBinary,
		Args:       []string{"-test.run=^$"},
		Env:        []string{"GO_WANT_HELPER_PROCESS=1"},
		Declares: []extension.DeclarationSpec{{
			Kind:    "fake",
			Methods: []extension.MethodSpec{{Name: "echo"}},
		}},
	}
	b, _ := json.MarshalIndent(man, "", "  ")
	if err := os.WriteFile(filepath.Join(extDir, "extension.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	sha := lockSHA
	if sha == "" {
		var err error
		sha, err = fileSHA256(testBinary)
		if err != nil {
			t.Fatal(err)
		}
	}
	lock := Lock{Extensions: map[string]LockEntry{}}
	if includeInLock {
		lock.Extensions[id] = LockEntry{Version: "1.0.0", SHA256: sha}
	}
	if err := lock.Save(filepath.Join(root, LockPath)); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDiscoverLoadsALockedExtensionEndToEnd(t *testing.T) {
	root := scaffold(t, "com.test.fake", "", true)

	set, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(set.Problems()) != 0 {
		t.Fatalf("unexpected problems: %+v", set.Problems())
	}
	if len(set.exts) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(set.exts))
	}
	t.Cleanup(set.CloseAll)

	reg := extension.NewRegistry(&recordingHost{})
	for _, e := range set.Extensions() {
		if err := reg.TryRegister(e); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := reg.Call(ctx, extension.CallRequest{
		Declaration: extension.Declaration{Kind: "fake", Name: "Customer"},
		Method:      "echo",
		Args:        []extension.Value{"hi"},
	})
	if err != nil {
		t.Fatalf("call through discovered extension: %v", err)
	}
	if got != "hi" {
		t.Fatalf("got %#v, want %q", got, "hi")
	}
}

func TestDiscoverIgnoresAnExtensionNotInTheLock(t *testing.T) {
	root := scaffold(t, "com.test.fake", "", false /* not in lock */)

	set, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.exts) != 0 || len(set.Problems()) != 0 {
		t.Fatalf("an unlocked extension must be silently ignored, got exts=%d problems=%+v", len(set.exts), set.Problems())
	}
}

func TestDiscoverRejectsAHashMismatch(t *testing.T) {
	root := scaffold(t, "com.test.fake", "deadbeef", true)

	set, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.exts) != 0 {
		t.Fatal("a changed binary must not load")
	}
	if len(set.Problems()) != 1 || !strings.Contains(set.Problems()[0].Message, "sha256") {
		t.Fatalf("expected a sha256 problem, got %+v", set.Problems())
	}
}

func TestDiscoverMissingManifestIsAProblemNotACrash(t *testing.T) {
	root := t.TempDir()
	lock := Lock{Extensions: map[string]LockEntry{
		"com.test.absent": {Version: "1.0.0", SHA256: "abc"},
	}}
	if err := lock.Save(filepath.Join(root, LockPath)); err != nil {
		t.Fatal(err)
	}
	set, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Problems()) != 1 || !strings.Contains(set.Problems()[0].Message, "not installed") {
		t.Fatalf("expected a not-installed problem, got %+v", set.Problems())
	}
}

func TestInspectReportsEveryLockEntry(t *testing.T) {
	root := scaffold(t, "com.test.fake", "", true)

	statuses, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	s := statuses[0]
	if !s.OK || s.ID != "com.test.fake" || s.Version != "1.0.0" || len(s.Kinds) != 1 || s.Kinds[0] != "fake" {
		t.Fatalf("unexpected status: %+v", s)
	}
}

func TestLoadLockMissingFileIsEmpty(t *testing.T) {
	l, err := LoadLock(filepath.Join(t.TempDir(), "nope.lock"))
	if err != nil {
		t.Fatalf("missing lock should not error: %v", err)
	}
	if len(l.Extensions) != 0 {
		t.Fatalf("expected empty lock, got %+v", l.Extensions)
	}
}
