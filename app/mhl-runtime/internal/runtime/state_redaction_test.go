package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/auth"
	"github.com/yanjustino/mhl-runtime/internal/runtime"
)

func TestCheckpointRedactsResolvedSecrets(t *testing.T) {
	secret := "checkpoint-secret-value"
	t.Setenv("MHL_CHECKPOINT_SECRET", secret)
	resolved, err := auth.Resolve(`env("MHL_CHECKPOINT_SECRET")`)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := runtime.NewStore(root).Save(&runtime.Checkpoint{Pipeline: "secure", Variables: map[string]string{"token": resolved}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, runtime.StateDirName, "secure.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || !strings.Contains(string(data), "[REDACTED]") {
		t.Fatalf("checkpoint leaked secret: %s", data)
	}
}
