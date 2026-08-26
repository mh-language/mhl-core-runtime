package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// TestInitScaffoldsAnImmediatelyRunnableProject proves `mhl init` writes a
// main.mh that both `mhl lint` and `mhl run` accept with no further edits —
// the scaffold's own "echo" agent needs no external binary or credential.
func TestInitScaffoldsAnImmediatelyRunnableProject(t *testing.T) {
	dir := t.TempDir()

	var initBuf bytes.Buffer
	if err := cli.Run([]string{"init", dir}, &initBuf); err != nil {
		t.Fatalf("init: %v", err)
	}
	path := filepath.Join(dir, "main.mh")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}

	var lintBuf bytes.Buffer
	if err := cli.Run([]string{"lint", dir}, &lintBuf); err != nil {
		t.Fatalf("lint: %v\n%s", err, lintBuf.String())
	}

	var runBuf bytes.Buffer
	if err := cli.Run([]string{"run", path, "--input", "name=World"}, &runBuf); err != nil {
		t.Fatalf("run: %v\n%s", err, runBuf.String())
	}
}

// TestInitRefusesToOverwriteAnExistingFile proves the fail-closed stance:
// a second `mhl init` in the same directory doesn't clobber the first
// main.mh, or any other pre-existing one.
func TestInitRefusesToOverwriteAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.mh")
	original := []byte("// hand-written, do not touch\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seeding main.mh: %v", err)
	}

	err := cli.Run([]string{"init", dir}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error for an existing main.mh, got nil")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading main.mh: %v", readErr)
	}
	if string(got) != string(original) {
		t.Errorf("main.mh was overwritten; got:\n%s", got)
	}
}

// TestInitDefaultsToTheCurrentDirectory proves `mhl init` with no argument
// scaffolds in ".", not some other implicit location.
func TestInitDefaultsToTheCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	if err := cli.Run([]string{"init"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat("main.mh"); err != nil {
		t.Fatalf("expected ./main.mh to exist: %v", err)
	}
}
