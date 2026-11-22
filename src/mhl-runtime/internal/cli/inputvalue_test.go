package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// An input given a value not of its declared type fails as invalid_inputs
// naming it (`invalid`), with a hint on the form it takes. On a resume nothing
// ran, so the run is still paused — resumable, and for a run paused on
// entering a stage, that stage.
func TestInputOfTheWrongTypeIsInvalidInputs(t *testing.T) {
	run := runInDir(t, t.TempDir(), `
workflow W {
    input n: number = 1
    step A {}
}
`)
	res := gateRun(t, run, "--input", "n=three")
	if res["kind"] != "invalid_inputs" || res["resumable"] != nil {
		t.Fatalf("fresh run = %v", res)
	}
	if inv, _ := res["invalid"].([]any); len(inv) != 1 || inv[0] != "n" {
		t.Fatalf("invalid = %v", res["invalid"])
	}
	if !strings.Contains(res["error"].(string), `"three" is not a valid number`) {
		t.Fatalf("the message must stay the coercion's: %v", res["error"])
	}
}

func TestInputOfTheWrongTypeOnAStageResume(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "verify.mh"), `
partial workflow V stage Verify {
    input cmd: string[]
    step Check { log("cmd=${cmd}") }
}
`)
	run := runInDir(t, dir, `
import "verify.mh"
partial workflow V {
    entry step Start {}
}
`)
	first := gateRun(t, run, "--session", "s")
	if first["paused"] != true {
		t.Fatalf("first leg = %v", first)
	}
	bad := gateRun(t, run, "--session", "s", "--resume", "--input", "cmd=ls")
	if bad["kind"] != "invalid_inputs" || bad["resumable"] != true || bad["stage"] != "Verify" {
		t.Fatalf("bad resume = %v", bad)
	}
	if hint, _ := bad["hint"].(string); !strings.Contains(hint, "JSON list") {
		t.Fatalf("hint = %q", hint)
	}
	// Still paused there: the corrected input continues the run.
	good := gateRun(t, run, "--session", "s", "--resume", "--input", `cmd=["ls"]`)
	if good["ok"] != true || good["paused"] == true {
		t.Fatalf("good resume = %v", good)
	}
}
