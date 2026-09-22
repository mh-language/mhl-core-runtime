package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// zeroFilledTarGz builds a real, validly-encoded tar.gz with n entries, each
// declaring (and actually containing) size bytes of zero-filled content —
// highly compressible, so building even a "bomb" of this shape stays fast in
// memory despite the nominal decompressed size being large.
func zeroFilledTarGz(t *testing.T, n int, size int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	zeros := make([]byte, size)
	for i := 0; i < n; i++ {
		hdr := &tar.Header{Name: "f" + strconv.Itoa(i), Typeflag: tar.TypeReg, Size: size, Mode: 0o644}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if size > 0 {
			if _, err := tw.Write(zeros); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	return buf.Bytes()
}

// zeroFilledZip is zeroFilledTarGz's zip equivalent.
func zeroFilledZip(t *testing.T, n int, size int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zeros := make([]byte, size)
	for i := 0; i < n; i++ {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "f" + strconv.Itoa(i), Method: zip.Deflate})
		if err != nil {
			t.Fatalf("CreateHeader: %v", err)
		}
		if size > 0 {
			if _, err := w.Write(zeros); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return buf.Bytes()
}

// dirIsEmpty reports whether dest holds no extracted files — used for a
// single-entry archive that must fail before writing anything.
func dirIsEmpty(t *testing.T, dest string) bool {
	t.Helper()
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	return len(entries) == 0
}

func TestExtractTarGzRejectsCumulativeBudget(t *testing.T) {
	// 3 entries at 100 MiB each (under the 256 MiB per-entry cap individually)
	// sum to 300 MiB, over the cumulative budget. The budget check runs before
	// the 3rd entry is opened for writing, so entries 1-2 (legitimately within
	// budget on their own) are extracted normally — extraction stops there,
	// like any other failure partway through a multi-file archive; the
	// invariant that matters is that the *rejected* entry (f2) never lands on
	// disk, checked below.
	data := zeroFilledTarGz(t, 3, 100<<20)
	dest := t.TempDir()
	err := extractTarGz(data, dest)
	if err == nil || !strings.Contains(err.Error(), "cumulative") {
		t.Fatalf("extractTarGz over cumulative budget = %v, want a cumulative-size error", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "f2")); !os.IsNotExist(err) {
		t.Fatalf("the entry that broke the cumulative budget was written anyway: stat err = %v", err)
	}
}

func TestExtractZipRejectsCumulativeBudget(t *testing.T) {
	data := zeroFilledZip(t, 3, 100<<20)
	dest := t.TempDir()
	err := extractZip(data, dest)
	if err == nil || !strings.Contains(err.Error(), "cumulative") {
		t.Fatalf("extractZip over cumulative budget = %v, want a cumulative-size error", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "f2")); !os.IsNotExist(err) {
		t.Fatalf("the entry that broke the cumulative budget was written anyway: stat err = %v", err)
	}
}

func TestExtractTarGzRejectsEntryCountBudget(t *testing.T) {
	data := zeroFilledTarGz(t, maxArchiveEntries+1, 0)
	dest := t.TempDir()
	err := extractTarGz(data, dest)
	if err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("extractTarGz over the entry-count budget = %v, want an entry-count error", err)
	}
}

func TestExtractZipRejectsEntryCountBudget(t *testing.T) {
	data := zeroFilledZip(t, maxArchiveEntries+1, 0)
	dest := t.TempDir()
	err := extractZip(data, dest)
	if err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("extractZip over the entry-count budget = %v, want an entry-count error", err)
	}
}

func TestExtractTarGzRejectsOversizedSingleEntry(t *testing.T) {
	data := zeroFilledTarGz(t, 1, maxArchiveBytes+1)
	dest := t.TempDir()
	err := extractTarGz(data, dest)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("extractTarGz with an oversized entry = %v, want an 'exceeds' error", err)
	}
	if !dirIsEmpty(t, dest) {
		t.Fatal("extractTarGz left a partial file behind after rejecting an oversized entry")
	}
}

func TestExtractZipRejectsOversizedSingleEntry(t *testing.T) {
	data := zeroFilledZip(t, 1, maxArchiveBytes+1)
	dest := t.TempDir()
	err := extractZip(data, dest)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("extractZip with an oversized entry = %v, want an 'exceeds' error", err)
	}
	if !dirIsEmpty(t, dest) {
		t.Fatal("extractZip left a partial file behind after rejecting an oversized entry")
	}
}

// TestExtractZipDoesNotSilentlyTruncate is the audit's exact finding: before
// the fix, extractZip capped the copy with io.LimitReader but discarded the
// count io.Copy returned, so an oversized entry was silently truncated on
// disk instead of failing. filepath.Join/os.Stat below confirm no truncated
// artifact survives a rejected extraction (covered structurally by the
// oversized-entry cases above; this test only pins the specific filename
// used so a regression is easy to spot in a future diff).
func TestExtractZipDoesNotSilentlyTruncate(t *testing.T) {
	data := zeroFilledZip(t, 1, maxArchiveBytes+1)
	dest := t.TempDir()
	if err := extractZip(data, dest); err == nil {
		t.Fatal("expected an error for an oversized zip entry")
	}
	if _, err := os.Stat(filepath.Join(dest, "f0")); !os.IsNotExist(err) {
		t.Fatalf("truncated file left on disk: stat err = %v", err)
	}
}

func TestParseArchiveSource(t *testing.T) {
	cases := []struct {
		in       string
		ok       bool
		url, sha string
		isZip    bool
	}{
		{in: "https://h/x/mhl-store-s3_linux_amd64.tar.gz", ok: true,
			url: "https://h/x/mhl-store-s3_linux_amd64.tar.gz"},
		{in: "https://h/x/ext.tgz", ok: true, url: "https://h/x/ext.tgz"},
		{in: "http://h/x/ext.zip", ok: true, url: "http://h/x/ext.zip", isZip: true},
		{in: "https://h/x/ext.tar.gz#sha256=ABCdef0123", ok: true,
			url: "https://h/x/ext.tar.gz", sha: "abcdef0123"},
		{in: "https://h/x/ext.tar.gz#v1.2.0", ok: false}, // non-sha fragment
		{in: "https://h/x/repo.git", ok: false},          // git, not an archive
		{in: "https://h/x/ext.tar", ok: false},           // uncompressed tar not supported
		{in: "ftp://h/x/ext.tar.gz", ok: false},          // not http(s)
		{in: "./local/ext.tar.gz", ok: false},
		{in: "", ok: false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			as, ok := parseArchiveSource(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if !c.ok {
				return
			}
			if as.URL != c.url || as.SHA256 != c.sha || as.isZip != c.isZip || as.Raw != c.in {
				t.Fatalf("got %+v", as)
			}
		})
	}
}

func TestSafeJoin(t *testing.T) {
	base := "/tmp/x"
	for _, ok := range []string{"bin/mhl", "extension.json", "./a/b"} {
		if _, err := safeJoin(base, ok); err != nil {
			t.Errorf("safeJoin(%q) unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"../escape", "a/../../escape", "/abs/evil"} {
		if got, err := safeJoin(base, bad); err == nil {
			t.Errorf("safeJoin(%q) = %q, want escape error", bad, got)
		}
	}
}
