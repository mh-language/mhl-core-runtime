package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// --state-dir moves the run's checkpoints out of ./.mhl/state: a failed run
// leaves nothing in the working directory, and a --resume given the same
// --state-dir continues from there.
func TestRunStateDirKeepsCheckpointsElsewhere(t *testing.T) {
	chdirTemp(t)
	state := t.TempDir()
	src := `
workflow W {
    input crash: bool = false
    mem visits = 0
    output: { visits: visits }
    step One { visits = visits + 1 }
    step Two { if (crash) { fail("stop") } }
}
`
	if err := os.WriteFile("w.mh", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cli.Run([]string{"run", "w.mh", "--state-dir", state, "--input", "crash=true"}, &out); err == nil {
		t.Fatalf("expected the run to fail at Two:\n%s", out.String())
	}
	if _, err := os.Stat(".mhl"); !os.IsNotExist(err) {
		t.Fatalf("the failed run wrote ./.mhl (stat err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(state, ".mhl", "state")); err != nil {
		t.Fatalf("no checkpoint under --state-dir: %v", err)
	}

	out.Reset()
	if err := cli.Run([]string{"run", "w.mh", "--state-dir", state, "--resume", "--input", "crash=false", "--format", "json"}, &out); err != nil {
		t.Fatalf("resume: %v\n%s", err, out.String())
	}
	var res struct {
		Vars    map[string]any `json:"vars"`
		Skipped []string       `json:"skipped"`
	}
	_ = json.Unmarshal(out.Bytes(), &res)
	if res.Vars["visits"] != float64(1) || len(res.Skipped) != 1 || res.Skipped[0] != "One" {
		t.Errorf("resume should skip One (visits stays 1):\n%s", out.String())
	}

	if err := cli.Run([]string{"run", "w.mh", "--state-dir"}, &out); err == nil || !strings.Contains(err.Error(), "--state-dir requires") {
		t.Errorf("--state-dir without a value: err = %v", err)
	}
}
