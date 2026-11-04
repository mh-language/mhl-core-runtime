package execsvc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// packTree is a small workflow tree exercising every kind of file the loader
// reads: an entry .mh, a library .mh in another directory, a prompt, a skill
// and a schema.
var packTree = map[string]string{
	"flows/greet.mh": `
import {Greeting, Brief} from "../shared/lib.mh"

workflow Greet {
    description: "Greets someone."
    input name: string
    step S {
        log(Greeting(name: name))
        log(Brief.path)
    }
}
`,
	"shared/lib.mh": `
export prompt Greeting(name: string) from "prompts/greeting.prompt.md"
export schema Brief from "schemas/brief.json"
`,
	"shared/prompts/greeting.prompt.md": "confidential greeting for ${name}",
	"shared/schemas/brief.json":         `{"type": "object"}`,
}

// A tree loaded through ParseFS/LoadFilesFS registers the same workflows,
// with the same input schemas, as the same tree on disk — and reads nothing
// from the disk: root holds no .mh or prompt at all.
func TestLoadFilesFSMatchesDirectory(t *testing.T) {
	disk := t.TempDir()
	mapFS := fstest.MapFS{}
	for name, content := range packTree {
		full := filepath.Join(disk, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		mapFS[name] = &fstest.MapFile{Data: []byte(content)}
	}
	fromDisk, err := execsvc.Load(disk)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	root := t.TempDir()
	files, err := execsvc.ParseFS(root, mapFS)
	if err != nil {
		t.Fatalf("ParseFS: %v", err)
	}
	fromFS, err := execsvc.LoadFilesFS(root, mapFS, files)
	if err != nil {
		t.Fatalf("LoadFilesFS: %v", err)
	}

	if len(fromFS) != len(fromDisk) {
		t.Fatalf("workflows: fs %d, disk %d", len(fromFS), len(fromDisk))
	}
	for name, d := range fromDisk {
		f, ok := fromFS[name]
		if !ok {
			t.Fatalf("workflow %q missing from the fs load", name)
		}
		dj, _ := json.Marshal(d.Pipeline.InputSchema())
		fj, _ := json.Marshal(f.Pipeline.InputSchema())
		if !bytes.Equal(dj, fj) || d.Pipeline.Description != f.Pipeline.Description {
			t.Errorf("%s: fs %s %q, disk %s %q", name, fj, f.Pipeline.Description, dj, d.Pipeline.Description)
		}
		if want := filepath.Join(root, "flows", "greet.mh"); f.File != want {
			t.Errorf("%s: File = %q, want %q", name, f.File, want)
		}
	}

	// Running it renders the prompt from memory, and the schema's path names
	// its place under root — where the host materializes it.
	var out bytes.Buffer
	_, err = execsvc.Run(execsvc.Request{
		Context:  context.Background(),
		Program:  fromFS["Greet"].Program,
		File:     fromFS["Greet"].File,
		Workflow: "Greet",
		Inputs:   map[string]any{"name": "Ada"},
		BaseDir:  t.TempDir(),
		Out:      &out,
	})
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	absRoot, _ := filepath.Abs(root)
	for _, want := range []string{"confidential greeting for Ada", filepath.Join(absRoot, "shared", "schemas", "brief.json")} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("run output missing %q:\n%s", want, out.String())
		}
	}
}

// An import that climbs out of the fs root is not silently read from the
// disk: it fails to resolve.
func TestLoadFilesFSDoesNotEscapeRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside.mh"), []byte(`export prompt P() from "x.prompt.md"`), 0o644); err != nil {
		t.Fatal(err)
	}
	mapFS := fstest.MapFS{"main.mh": &fstest.MapFile{Data: []byte(`
import {P} from "../outside.mh"
pipeline Main { step S { log(P()) } }
`)}}
	files, err := execsvc.ParseFS(root, mapFS)
	if err != nil {
		t.Fatal(err)
	}
	_, err = execsvc.LoadFilesFS(root, mapFS, files)
	if err == nil || !strings.Contains(err.Error(), "outside the workflows pack") {
		t.Fatalf("err = %v, want an outside-the-pack error", err)
	}
}
