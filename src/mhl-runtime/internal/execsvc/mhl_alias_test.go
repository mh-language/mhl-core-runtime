package execsvc_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

const aliasWorkflow = `
workflow FromMhl {
    step S {
        log("mhl")
    }
}
`

// `mhl serve <dir>` registers a workflow declared in a .mhl file — ".mhl" is
// an alias of ".mh" — whether the tree is on disk or in a pack (ParseFS).
func TestLoadRegistersMhlFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "flow.mhl"), []byte(aliasWorkflow), 0o644); err != nil {
		t.Fatal(err)
	}
	workflows, err := execsvc.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := workflows["FromMhl"]; !ok {
		t.Fatalf("FromMhl not registered from flow.mhl: %+v", workflows)
	}

	files, err := execsvc.ParseFS(t.TempDir(), fstest.MapFS{"flows/flow.mhl": {Data: []byte(aliasWorkflow)}})
	if err != nil {
		t.Fatalf("ParseFS: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ParseFS found %d files, want the one .mhl", len(files))
	}
}
