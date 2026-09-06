package runtime_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

// Separate writer/reader processes prove references survive an empty registry;
// they also isolate tiny explicit secrets from other tests in this package.
func TestExplicitCredentialCheckpointAcrossProcesses(t *testing.T) {
	for _, value := range []string{"x9!", "12345678", "true", "  padded-checkpoint-secret  ", " "} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			for _, phase := range []string{"write", "read"} {
				cmd := exec.Command(os.Args[0], "-test.run=^TestExplicitCredentialCheckpointHelper$")
				cmd.Env = append(os.Environ(), "MHL_R2_PHASE="+phase, "MHL_R2_ROOT="+root, "MHL_R2_VALUE="+value)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s: %v\n%s", phase, err, out)
				}
			}
		})
	}
}

func TestExplicitCredentialCheckpointHelper(t *testing.T) {
	phase := os.Getenv("MHL_R2_PHASE")
	if phase == "" {
		t.Skip("subprocess helper")
	}
	root, value := os.Getenv("MHL_R2_ROOT"), os.Getenv("MHL_R2_VALUE")
	t.Setenv("MHL_R2_CREDENTIAL", value)
	store := runtime.NewStore(root)
	if phase == "write" {
		resolved, err := auth.Resolve(`env("MHL_R2_CREDENTIAL")`)
		if err != nil {
			t.Fatal(err)
		}
		vars := map[string]any{"token": resolved, "nested": map[string]any{"array": []any{resolved}}, "number": 42, "boolean": true}
		clean := runtime.RedactVars(vars)
		if clean["token"] != "[REDACTED]" || clean["number"] != 42 || clean["boolean"] != true {
			t.Fatal("output redaction changed the contract")
		}
		if err := store.Save(&runtime.Checkpoint{Pipeline: "explicit", Variables: vars}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, runtime.StateDirName, "explicit.json"))
		if err != nil {
			t.Fatal(err)
		}
		var disk runtime.Checkpoint
		if err := json.Unmarshal(data, &disk); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"__mhl_secret_ref__": `env("MHL_R2_CREDENTIAL")`}
		if !reflect.DeepEqual(disk.Variables["token"], want) {
			t.Fatal("checkpoint did not persist the exact reference")
		}
		if !reflect.DeepEqual(disk.Variables["nested"].(map[string]any)["array"].([]any)[0], want) {
			t.Fatal("nested checkpoint reference missing")
		}
		return
	}
	cp, ok, err := store.Load("explicit")
	if err != nil || !ok {
		t.Fatalf("initial load: %v, %v", ok, err)
	}
	if cp.Variables["token"] != value {
		t.Fatal("resume changed the exact credential value")
	}
	// Rotation proves the reader resolves the reference instead of restoring
	// either the original literal or a dead redaction mask.
	const rotated = "rotated-credential-for-reader"
	t.Setenv("MHL_R2_CREDENTIAL", rotated)
	cp, ok, err = store.Load("explicit")
	if err != nil || !ok {
		t.Fatalf("load: %v, %v", ok, err)
	}
	if cp.Variables["token"] != rotated || cp.Variables["nested"].(map[string]any)["array"].([]any)[0] != rotated {
		t.Fatal("resume did not resolve the rotated credential")
	}
}

func TestAmbiguousCredentialCheckpointRefusesResume(t *testing.T) {
	const value = "same-content-different-checkpoint-references"
	t.Setenv("MHL_R2_FIRST", value)
	t.Setenv("MHL_R2_SECOND", value)
	for _, ref := range []string{`env("MHL_R2_FIRST")`, `env("MHL_R2_SECOND")`} {
		if _, err := auth.Resolve(ref); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	store := runtime.NewStore(root)
	if err := store.Save(&runtime.Checkpoint{Pipeline: "ambiguous", Variables: map[string]any{"nested": []any{value}}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, runtime.StateDirName, "ambiguous.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), value) {
		t.Fatal("ambiguous checkpoint exposed credential")
	}
	if _, _, err := store.Load("ambiguous"); err == nil || !strings.Contains(err.Error(), "ambiguous credential reference") {
		t.Fatalf("want explicit ambiguity error, got %v", err)
	}
}
