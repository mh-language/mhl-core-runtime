package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --no-cache calls a cached agent again, and its fresh reply replaces the
// cached one for the next run.
func TestRunNoCacheCallsTheAgentAgain(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "agent.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho x >> "+counter+"\nwc -l < "+counter+" | tr -d ' '\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := runInDir(t, dir, `
agent A {
    command: "`+filepath.ToSlash(script)+`"
    cache: { ttl: 10m, storage: "disk" }
}
workflow W {
    step S { log("reply=${A.run(prompt: "p").trim()}") }
}
`)
	for i, c := range []struct {
		args []string
		want string
	}{{nil, "reply=1"}, {nil, "reply=1"}, {[]string{"--no-cache"}, "reply=2"}, {nil, "reply=2"}} {
		out, err := run(c.args...)
		if err != nil || !strings.Contains(out, c.want) {
			t.Fatalf("run %d %v: err=%v, want %q in:\n%s", i, c.args, err, c.want, out)
		}
	}
}
