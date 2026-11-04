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

// writePack seals one workflow under key into dir/w.dmpk.
func writePack(t *testing.T, dir string, key []byte) string {
	t.Helper()
	raw, err := pack.Seal([]pack.File{{Path: "w.mh", Content: []byte("pipeline P { step S { log(\"hi\") } }\n")}}, key)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "w.dmpk")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Every misconfiguration or bad pack fails `serve mcp` before anything is
// served, with an error that names the problem but never the key.
func TestServeWorkflowsPackRejects(t *testing.T) {
	dir := t.TempDir()
	key := bytes.Repeat([]byte{7}, pack.KeySize)
	good := writePack(t, dir, key)
	tampered := filepath.Join(dir, "tampered.dmpk")
	raw, _ := os.ReadFile(good)
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(tampered, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	wrong := bytes.Repeat([]byte{8}, pack.KeySize)

	for _, c := range []struct {
		name string
		key  string // "" leaves the env var unset
		args []string
		want string
	}{
		{"no key", "", []string{"--http", "--workflows-pack", good}, "needs its key"},
		{"not hex", "zz", []string{"--http", "--workflows-pack", good}, "hex characters"},
		{"short key", hex.EncodeToString(key[:16]), []string{"--http", "--workflows-pack", good}, "hex characters"},
		{"wrong key", hex.EncodeToString(wrong), []string{"--http", "--workflows-pack", good}, "does not authenticate"},
		{"tampered", hex.EncodeToString(key), []string{"--http", "--workflows-pack", tampered}, "does not authenticate"},
		{"missing file", hex.EncodeToString(key), []string{"--http", "--workflows-pack", filepath.Join(dir, "nope.dmpk")}, "--workflows-pack"},
		{"stdio", hex.EncodeToString(key), []string{"--workflows-pack", good}, "requires --http"},
		{"key without pack", hex.EncodeToString(key), []string{"--http"}, "no --workflows-pack"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("MHL_SERVE_WORKFLOWS_PACK", "")
			if c.key != "" {
				t.Setenv("MHL_SERVE_WORKFLOWS_KEY", c.key)
			} else {
				t.Setenv("MHL_SERVE_WORKFLOWS_KEY", "")
				os.Unsetenv("MHL_SERVE_WORKFLOWS_KEY")
			}
			args := append([]string{"serve", "mcp", "--addr", "127.0.0.1:0"}, c.args...)
			err := cli.Run(append(args, dir), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}
			if c.key != "" && strings.Contains(err.Error(), c.key) {
				t.Errorf("error echoes the key: %v", err)
			}
			// Removed from the environment whatever happened next, so no
			// subprocess (or env() call) can see it.
			if v, ok := os.LookupEnv("MHL_SERVE_WORKFLOWS_KEY"); ok {
				t.Errorf("key still in the environment: %q", v)
			}
		})
	}
}
