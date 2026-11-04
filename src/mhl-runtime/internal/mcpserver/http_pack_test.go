package mcpserver_test

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/mcpserver"
	"github.com/mh-language/mhl-core-runtime/internal/pack"
)

// packWF is a two-directory tree whose prompt lives only in the pack.
var packWF = map[string]string{
	"flows/greet.mh": `
import {Greeting} from "../shared/lib.mh"

workflow Greet {
    description: "Greets someone."
    input name: string
    var text = ""
    output: { text: text }
    step S { text = Greeting(name: name) }
}
`,
	"shared/lib.mh":                     `export prompt Greeting(name: string) from "greeting.prompt.md"`,
	"shared/greeting.prompt.md":         "confidential greeting for ${name}",
	"shared/schemas/unused.schema.json": `{"type": "object"}`,
}

func toolsList(t *testing.T, url string) any {
	t.Helper()
	sid := initHTTPSession(t, url)
	_, body := postMCP(t, url, sid, rpcMap(2, "tools/list", nil), nil)
	return body["result"]
}

// Served from an encrypted pack, the endpoint lists exactly the tools — same
// schemas — the same tree on disk does, runs them with the prompt read from
// the pack, and the root directory never receives a .mh or prompt file.
func TestHTTPWorkflowsPackMatchesDirectory(t *testing.T) {
	disk := t.TempDir()
	var files []pack.File
	for name, body := range packWF {
		full := filepath.Join(disk, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, pack.File{Path: name, Content: []byte(body)})
	}
	dirHandler, err := mcpserver.Handler(mcpserver.HTTPConfig{Dir: disk}, io.Discard)
	if err != nil {
		t.Fatalf("Handler (dir): %v", err)
	}
	dirTS := httptest.NewServer(dirHandler)
	t.Cleanup(dirTS.Close)

	key := make([]byte, pack.KeySize)
	raw, err := pack.Seal(files, key)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := pack.Open(raw, key)
	if err != nil {
		t.Fatal(err)
	}
	fsys, err := pack.NewFS(opened)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	packHandler, err := mcpserver.Handler(mcpserver.HTTPConfig{Dir: root, FS: fsys}, io.Discard)
	if err != nil {
		t.Fatalf("Handler (pack): %v", err)
	}
	packTS := httptest.NewServer(packHandler)
	t.Cleanup(packTS.Close)

	fromDir, fromPack := toolsList(t, dirTS.URL), toolsList(t, packTS.URL)
	if !reflect.DeepEqual(fromDir, fromPack) {
		d, _ := json.Marshal(fromDir)
		p, _ := json.Marshal(fromPack)
		t.Fatalf("tools/list differs:\ndir:  %s\npack: %s", d, p)
	}

	sid := initHTTPSession(t, packTS.URL)
	res := callTool(t, packTS.URL, sid, 3, "Greet", map[string]any{"name": "Ada"})
	if got := structured(t, res)["text"]; got != "confidential greeting for Ada" {
		t.Errorf("Greet text = %v", got)
	}

	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".mh") || strings.HasSuffix(p, ".prompt.md")) {
			t.Errorf("pack mode wrote %s to the root", p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
