package nativeops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/features/nativeops"
)

func TestWriteThenReadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "out.txt")
	ok, err := nativeops.Write(path, "hello")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !ok {
		t.Errorf("Write returned false")
	}
	got, err := nativeops.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "hello" {
		t.Errorf("Read = %q, want %q", got, "hello")
	}
}

func TestReadMissingFileErrors(t *testing.T) {
	_, err := nativeops.Read(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected an error reading a missing file")
	}
}

func TestWriteCreatesMissingParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "out.txt")
	if _, err := nativeops.Write(path, "x"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestAppendCreatesFileWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.txt")
	ok, err := nativeops.Append(path, "first line\n")
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if !ok {
		t.Errorf("Append returned false")
	}
	got, err := nativeops.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "first line\n" {
		t.Errorf("Read = %q, want %q", got, "first line\n")
	}
}

func TestAppendAddsToExistingContentWithoutOverwriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.txt")
	if _, err := nativeops.Append(path, "first line\n"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := nativeops.Append(path, "second line\n"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := nativeops.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := "first line\nsecond line\n"
	if got != want {
		t.Errorf("Read = %q, want %q", got, want)
	}
}

func TestAppendCreatesMissingParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "log.txt")
	if _, err := nativeops.Append(path, "x"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}
