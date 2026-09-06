package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

// A checkpoint must not persist a resolved secret value, but — for a secret
// with a known credential reference — it stores that reference so a
// fresh-process --resume re-resolves the live value instead of restoring a
// dead "[REDACTED]" mask.
func TestCheckpointStoresCredentialReferenceAndRehydratesOnLoad(t *testing.T) {
	secret := "checkpoint-secret-value"
	t.Setenv("MHL_CHECKPOINT_SECRET", secret)
	resolved, err := auth.Resolve(`env("MHL_CHECKPOINT_SECRET")`)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := runtime.NewStore(root).Save(&runtime.Checkpoint{Pipeline: "secure", Variables: map[string]any{"token": resolved}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, runtime.StateDirName, "secure.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("checkpoint persisted the resolved secret: %s", data)
	}
	if !strings.Contains(string(data), `env(\"MHL_CHECKPOINT_SECRET\")`) {
		t.Fatalf("checkpoint did not store the credential reference: %s", data)
	}

	cp, ok, err := runtime.NewStore(root).Load("secure")
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if cp.Variables["token"] != secret {
		t.Fatalf("resume did not rehydrate the secret: got %v", cp.Variables["token"])
	}
}

// A secret with no known reference (a literal bearer token handed to http.*)
// is still masked in a checkpoint — there is nothing to re-resolve.
func TestCheckpointMasksReferencelessSecret(t *testing.T) {
	secret := "referenceless-bearer-token-xyz"
	auth.Register(secret)
	root := t.TempDir()
	if err := runtime.NewStore(root).Save(&runtime.Checkpoint{Pipeline: "masked", Variables: map[string]any{"auth": secret}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, runtime.StateDirName, "masked.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || !strings.Contains(string(data), "[REDACTED]") {
		t.Fatalf("referenceless secret not masked: %s", data)
	}
}

// A resumed run must not silently proceed with a credential that no longer
// resolves.
func TestCheckpointResumeFailsWhenReferenceUnresolvable(t *testing.T) {
	secret := "vanishing-secret-value"
	t.Setenv("MHL_VANISHING_SECRET", secret)
	resolved, err := auth.Resolve(`env("MHL_VANISHING_SECRET")`)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := runtime.NewStore(root).Save(&runtime.Checkpoint{Pipeline: "gone", Variables: map[string]any{"token": resolved}}); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("MHL_VANISHING_SECRET")
	if _, _, err := runtime.NewStore(root).Load("gone"); err == nil {
		t.Fatal("Load should fail when a stored credential reference no longer resolves")
	}
}

func TestRedactVarsRecursesIntoObjectsAndArrays(t *testing.T) {
	secret := "SYNTHETIC_ASSESSMENT_SECRET_123"
	auth.Register(secret)

	vars := map[string]any{
		"scalar": secret,
		"object": map[string]any{
			"token":  secret,
			"nested": map[string]any{"deep": secret},
		},
		"array":        []any{secret, "safe"},
		"arrayOfObj":   []any{map[string]any{"k": secret}},
		"strings":      []string{secret},
		"passthrough":  42,
		"passthroughB": true,
	}

	clean := runtime.RedactVars(vars)

	blob := mustJSON(t, clean)
	if strings.Contains(blob, secret) {
		t.Fatalf("recursive redaction missed a secret: %s", blob)
	}
	// map[any]any (a decoded bare-object literal) is redacted too.
	gotMap := runtime.RedactValue(map[any]any{"k": secret}).(map[any]any)
	if gotMap["k"] != "[REDACTED]" {
		t.Fatalf("map[any]any not redacted: %#v", gotMap)
	}
	// The original must be left untouched (redactValue returns copies).
	if got := vars["object"].(map[string]any)["token"]; got != secret {
		t.Fatalf("RedactVars mutated its input: %v", got)
	}
	if clean["passthrough"] != 42 || clean["passthroughB"] != true {
		t.Fatalf("RedactVars altered non-string scalars: %#v", clean)
	}
}

func TestCheckpointRedactsNestedResolvedSecrets(t *testing.T) {
	secret := "checkpoint-nested-secret-value"
	t.Setenv("MHL_CHECKPOINT_NESTED_SECRET", secret)
	resolved, err := auth.Resolve(`env("MHL_CHECKPOINT_NESTED_SECRET")`)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cp := &runtime.Checkpoint{
		Pipeline: "secure-nested",
		Variables: map[string]any{
			"creds": map[string]any{"token": resolved},
			"list":  []any{resolved},
		},
	}
	if err := runtime.NewStore(root).Save(cp); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, runtime.StateDirName, "secure-nested.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("checkpoint leaked nested secret: %s", data)
	}

	got, ok, err := runtime.NewStore(root).Load("secure-nested")
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if got.Variables["creds"].(map[string]any)["token"] != secret {
		t.Fatalf("nested secret not rehydrated: %v", got.Variables["creds"])
	}
	if got.Variables["list"].([]any)[0] != secret {
		t.Fatalf("array secret not rehydrated: %v", got.Variables["list"])
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
