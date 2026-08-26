package memory_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/memory"
)

func TestAppendCreatesFileAndWritesLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	if err := memory.Append(path, "first entry"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "first entry\n" {
		t.Errorf("content = %q", string(content))
	}
}

func TestAppendAddsSubsequentLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	if err := memory.Append(path, "line 1"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := memory.Append(path, "line 2"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "line 1\nline 2\n" {
		t.Errorf("content = %q", string(content))
	}
}

func TestAppendCreatesMissingParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "nested", "audit.log")

	if err := memory.Append(path, "entry"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist: %v", err)
	}
}
