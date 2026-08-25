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

func TestDeleteRemovesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if _, err := nativeops.Write(path, "hello"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	ok, err := nativeops.Delete(path)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !ok {
		t.Errorf("Delete returned false")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be gone, stat err = %v", err)
	}
}

func TestDeleteMissingFileErrors(t *testing.T) {
	_, err := nativeops.Delete(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected an error deleting a missing file")
	}
}

func TestListReturnsJoinedPathsSortedByName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	got, err := nativeops.List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{
		filepath.Join(dir, "a.txt"),
		filepath.Join(dir, "b.txt"),
		filepath.Join(dir, "sub"),
	}
	if len(got) != len(want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestListMissingDirErrors(t *testing.T) {
	_, err := nativeops.List(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected an error listing a missing directory")
	}
}

func TestJoinMatchesFilepathJoin(t *testing.T) {
	got := nativeops.Join("a", "b", "c.txt")
	want := filepath.Join("a", "b", "c.txt")
	if got != want {
		t.Errorf("Join = %q, want %q", got, want)
	}
}
