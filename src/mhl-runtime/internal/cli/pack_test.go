package cli_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
	"github.com/mh-language/mhl-core-runtime/internal/pack"
)

// writeTree writes files (slash paths) under a new temp dir.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var packCLITree = map[string]string{
	"flows/greet.mh":           "import {G} from \"../shared/lib.mh\"\nworkflow Greet { input name: string\n step S { log(G(name: name)) } }\n",
	"shared/lib.mh":            "export prompt G(name: string) from \"g.prompt.md\"\n",
	"shared/g.prompt.md":       "hi ${name}",
	"shared/a.json":            "{}",
	".mhl/state/leftover.json": "not packed",
	"shared/.DS_Store":         "not packed",
}

// `mhl pack` with a key from the env writes a 0600 pack that opens with
// that key, holds every non-hidden file sorted by path, and reports the
// workflows it verified.
func TestPackWithEnvKey(t *testing.T) {
	dir := writeTree(t, packCLITree)
	key := bytes.Repeat([]byte{3}, pack.KeySize)
	t.Setenv("MHL_SERVE_WORKFLOWS_KEY", hex.EncodeToString(key))
	out := filepath.Join(t.TempDir(), "w.dmpk")

	var buf bytes.Buffer
	if err := cli.Run([]string{"pack", dir, "-o", out}, &buf); err != nil {
		t.Fatalf("pack: %v", err)
	}
	if !strings.Contains(buf.String(), "packed 4 file(s), 1 workflow(s) (Greet)") {
		t.Errorf("summary = %q", buf.String())
	}
	if strings.Contains(buf.String(), hex.EncodeToString(key)) {
		t.Error("summary printed the key")
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("pack mode = %o, want 600", perm)
	}
	raw, _ := os.ReadFile(out)
	files, err := pack.Open(raw, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if got, want := strings.Join(paths, " "), "flows/greet.mh shared/a.json shared/g.prompt.md shared/lib.mh"; got != want {
		t.Errorf("paths = %q, want %q", got, want)
	}
}

// --new-key prints only the generated key on stdout, and that key opens the
// pack.
func TestPackNewKey(t *testing.T) {
	dir := writeTree(t, packCLITree)
	t.Setenv("MHL_SERVE_WORKFLOWS_KEY", "")
	out := filepath.Join(t.TempDir(), "w.dmpk")

	var buf bytes.Buffer
	if err := cli.Run([]string{"pack", dir, "-o", out, "--new-key"}, &buf); err != nil {
		t.Fatalf("pack: %v", err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(buf.String()))
	if err != nil || len(key) != pack.KeySize {
		t.Fatalf("stdout = %q, want only a 64-hex key", buf.String())
	}
	raw, _ := os.ReadFile(out)
	if _, err := pack.Open(raw, key); err != nil {
		t.Fatalf("printed key does not open the pack: %v", err)
	}
}

// A pack placed inside the directory it seals is not packed into itself.
func TestPackSkipsItsOwnOutput(t *testing.T) {
	dir := writeTree(t, packCLITree)
	key := bytes.Repeat([]byte{4}, pack.KeySize)
	t.Setenv("MHL_SERVE_WORKFLOWS_KEY", hex.EncodeToString(key))
	out := filepath.Join(dir, "w.dmpk")
	for range 2 {
		if err := cli.Run([]string{"pack", dir, "-o", out}, &bytes.Buffer{}); err != nil {
			t.Fatalf("pack: %v", err)
		}
	}
	raw, _ := os.ReadFile(out)
	files, err := pack.Open(raw, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "w.dmpk" {
			t.Fatal("the pack contains itself")
		}
	}
}

func TestPackRejects(t *testing.T) {
	good := hex.EncodeToString(bytes.Repeat([]byte{5}, pack.KeySize))
	broken := writeTree(t, map[string]string{"bad.mh": "workflow {{{"})
	noWorkflow := writeTree(t, map[string]string{"lib.mh": "export prompt P() from \"p.prompt.md\"", "p.prompt.md": "x"})
	missingPrompt := writeTree(t, map[string]string{"w.mh": "prompt P() from \"nope.prompt.md\"\nworkflow W { step S { log(P()) } }"})
	withLink := writeTree(t, packCLITree)
	if err := os.Symlink("g.prompt.md", filepath.Join(withLink, "shared", "link.md")); err != nil {
		t.Fatal(err)
	}
	ok := writeTree(t, packCLITree)

	for _, c := range []struct {
		name, key string
		args      []string
		want      string
	}{
		{"no key", "", []string{"pack", ok, "-o", "x"}, "no key"},
		{"bad key", "abcd", []string{"pack", ok, "-o", "x"}, "hex characters"},
		{"new-key with env key", good, []string{"pack", ok, "-o", "x", "--new-key"}, "conflicts"},
		{"no output", good, []string{"pack", ok}, "usage"},
		{"unknown flag", good, []string{"pack", ok, "-o", "x", "--key", "k"}, "unexpected argument"},
		{"parse error", good, []string{"pack", broken, "-o", "x"}, "bad.mh"},
		{"no workflow", good, []string{"pack", noWorkflow, "-o", "x"}, "no pipeline or workflow"},
		{"missing prompt", good, []string{"pack", missingPrompt, "-o", "x"}, "nope.prompt.md"},
		{"symlink", good, []string{"pack", withLink, "-o", "x"}, "symlinks"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MHL_SERVE_WORKFLOWS_KEY", c.key)
			outDir := t.TempDir()
			args := append([]string(nil), c.args...)
			for i, a := range args {
				if a == "x" {
					args[i] = filepath.Join(outDir, "x.dmpk")
				}
			}
			err := cli.Run(args, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
			if entries, _ := os.ReadDir(outDir); len(entries) != 0 {
				t.Errorf("a failed pack left %d file(s) behind", len(entries))
			}
		})
	}
}
