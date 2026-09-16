package nativeops_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
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

func TestAppendWriterFlushesCompleteLinesAndBuffersTheTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "codex.log")
	w := nativeops.AppendWriter(path)

	// A chunk ending mid-line: the complete line is on disk, the tail is not.
	if _, err := w.Write([]byte("line one\nline two, par")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := nativeops.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "line one\n" {
		t.Fatalf("after first Write = %q, want %q (tail must stay buffered)", got, "line one\n")
	}

	// The newline that finishes the buffered tail flushes it.
	if _, err := w.Write([]byte("tial\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// A newline-less tail is only flushed by Close.
	if _, err := w.Write([]byte("no newline here")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got, _ := nativeops.Read(path); got != "line one\nline two, partial\n" {
		t.Fatalf("before Close = %q, want %q", got, "line one\nline two, partial\n")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, _ := nativeops.Read(path); got != "line one\nline two, partial\nno newline here" {
		t.Fatalf("after Close = %q, want full content", got)
	}
}

func TestAppendWriterNeverWritesWithoutOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unused.log")
	w := nativeops.AppendWriter(path)
	if _, err := w.Write(nil); err != nil {
		t.Fatalf("Write(nil): %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no file, stat err = %v", err)
	}
}

func TestAppendWriterConcurrentWritersNeverTearALine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.log")
	const perWriter = 200
	var wg sync.WaitGroup
	for id := 0; id < 4; id++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w := nativeops.AppendWriter(path)
			defer w.Close()
			line := fmt.Sprintf("writer-%d-%s\n", id, strings.Repeat("x", 5000))
			for i := 0; i < perWriter; i++ {
				if _, err := w.Write([]byte(line)); err != nil {
					t.Errorf("Write: %v", err)
					return
				}
			}
		}(id)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	if last := lines[len(lines)-1]; last == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != 4*perWriter {
		t.Fatalf("got %d lines, want %d", len(lines), 4*perWriter)
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, "x\n") || !strings.HasPrefix(l, "writer-") {
			t.Fatalf("torn line: %.40q...", l)
		}
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
	// path.Join (always "/", unlike path/filepath which is OS-aware) against
	// dir pre-normalized the same way — an independently-computed forward
	// slash expectation, not just re-deriving what List itself did.
	slashDir := filepath.ToSlash(dir)
	want := []string{
		path.Join(slashDir, "a.txt"),
		path.Join(slashDir, "b.txt"),
		path.Join(slashDir, "sub"),
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

// TestListAndJoinNeverReturnBackslashes pins the actual bug: .mh code (e.g.
// workflows/work_item/actions.mh's WorkItemActions.list, via
// entry.replace("projects/", "")) assumes every fs/dir path it gets back
// uses "/", per workflows/shared/core/paths.mh's own documented contract.
// dir.list("projects") on Windows used to hand back "projects\<id>"
// (filepath.Join is OS-aware), which silently failed that replace, dropping
// every real work-item from a freshly-created list — reproduced for real on
// Windows, not hypothetical. This can't force GOOS=windows-only separator
// behavior running on a non-Windows CI machine (filepath.ToSlash is a no-op
// where "/" is already native), but it still pins the contract these two
// functions must not regress on any platform.
func TestListAndJoinNeverReturnBackslashes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	got, err := nativeops.List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, p := range got {
		if strings.Contains(p, `\`) {
			t.Errorf("List returned a backslash-separated path: %q", p)
		}
	}

	if j := nativeops.Join("a", "b", "c.txt"); strings.Contains(j, `\`) {
		t.Errorf("Join returned a backslash-separated path: %q", j)
	}
}

func TestJoinReturnsForwardSlashJoinedPath(t *testing.T) {
	got := nativeops.Join("a", "b", "c.txt")
	want := path.Join("a", "b", "c.txt") // independently computed, always "/"
	if got != want {
		t.Errorf("Join = %q, want %q", got, want)
	}
}
