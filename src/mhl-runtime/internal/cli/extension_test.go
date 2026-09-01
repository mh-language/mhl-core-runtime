package cli_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// tgzExtension builds a gzipped tar of a minimal extension rooted at rootDir/
// (e.g. "mhl-demo_linux_amd64") — manifest, declarations, and a stub
// executable — and returns the bytes plus their sha256.
func tgzExtension(t *testing.T, rootDir string) ([]byte, string) {
	t.Helper()
	files := map[string]struct {
		body []byte
		mode int64
	}{
		rootDir + "/extension.json":    {[]byte(`{"id":"dev.demo.x","version":"0.9.0","api_version":"1","executable":"bin/x","declarations_file":"declarations.json"}`), 0o644},
		rootDir + "/declarations.json": {[]byte(`[{"kind":"demo"}]`), 0o644},
		rootDir + "/bin/x":             {[]byte("#!/bin/sh\nexit 0\n"), 0o755},
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// TestExtensionInstallFromArchiveURL: `mhl extension install <url>.tar.gz`
// downloads the archive, finds the manifest in its single top-level directory,
// vendors it, and pins the executable — the shape `make release` publishes. An
// #sha256= that matches passes and is recorded; one that does not fails closed.
func TestExtensionInstallFromArchiveURL(t *testing.T) {
	body, sum := tgzExtension(t, "mhl-demo_linux_amd64")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	url := srv.URL + "/mhl-demo_linux_amd64.tar.gz"

	// Wrong digest → refused, nothing vendored.
	proj := t.TempDir()
	t.Chdir(proj)
	var b0 bytes.Buffer
	if err := cli.Run([]string{"extension", "install", url + "#sha256=" + strings.Repeat("0", 64)}, &b0); err == nil {
		t.Fatal("install should fail on a sha256 mismatch")
	}
	if _, err := os.Stat(filepath.Join(proj, ".mhl", "extensions.lock")); err == nil {
		t.Fatal("a failed install must not write the lock")
	}

	// Correct digest → installed, provenance recorded.
	var b1 bytes.Buffer
	if err := cli.Run([]string{"extension", "install", url + "#sha256=" + sum}, &b1); err != nil {
		t.Fatalf("install from archive URL: %v\n%s", err, b1.String())
	}
	if _, err := os.Stat(filepath.Join(proj, ".mhl", "extensions", "dev.demo.x", "bin", "x")); err != nil {
		t.Fatalf("executable not vendored from the archive: %v", err)
	}
	lock, _ := os.ReadFile(filepath.Join(proj, ".mhl", "extensions.lock"))
	s := string(lock)
	if !strings.Contains(s, `"0.9.0"`) || !strings.Contains(s, `"sha256"`) || !strings.Contains(s, `"source": "`+url+"#sha256="+sum+`"`) {
		t.Fatalf("lock missing version / executable sha / source:\n%s", s)
	}

	var db bytes.Buffer
	if err := cli.Run([]string{"extension", "doctor"}, &db); err != nil {
		t.Fatalf("doctor after archive install: %v\n%s", err, db.String())
	}

	// No digest given → still installs (executable hash is the trust anchor).
	proj2 := t.TempDir()
	t.Chdir(proj2)
	var b2 bytes.Buffer
	if err := cli.Run([]string{"extension", "install", url}, &b2); err != nil {
		t.Fatalf("install without #sha256: %v\n%s", err, b2.String())
	}
}

// TestExtensionInstallMultiPlatformPicksHostBinary: a source dir holding one
// binary per platform (bin/<name>-<goos>-<goarch>) plus an unchanged
// extension.json installs only the running host's binary, and doctor is happy.
func TestExtensionInstallMultiPlatformPicksHostBinary(t *testing.T) {
	src := filepath.Join(t.TempDir(), "pkg")
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "extension.json"), []byte(
		`{"id":"dev.demo.mp","version":"1.0.0","api_version":"1","executable":"bin/mp","declarations":[{"kind":"demo"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	host := "bin/mp-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		host += ".exe"
	}
	for _, rel := range []string{"bin/mp-linux-amd64", "bin/mp-linux-arm64", "bin/mp-darwin-arm64", "bin/mp-windows-amd64.exe", host} {
		if err := os.WriteFile(filepath.Join(src, rel), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	proj := t.TempDir()
	t.Chdir(proj)
	var buf bytes.Buffer
	if err := cli.Run([]string{"extension", "install", src}, &buf); err != nil {
		t.Fatalf("install multi-platform: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "selected "+runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("no 'selected' line: %q", buf.String())
	}

	binDir := filepath.Join(proj, ".mhl", "extensions", "dev.demo.mp", "bin")
	entries, err := os.ReadDir(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(host) {
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		t.Fatalf("vendored bin/ = %v, want just %q", got, filepath.Base(host))
	}

	var db bytes.Buffer
	if err := cli.Run([]string{"extension", "doctor"}, &db); err != nil {
		t.Fatalf("doctor after multi-platform install: %v\n%s", err, db.String())
	}
}

// TestExtensionInstallMultiPlatformNoHostBinary: an unhelpful platform set
// fails before writing anything, and names what is available.
func TestExtensionInstallMultiPlatformNoHostBinary(t *testing.T) {
	src := filepath.Join(t.TempDir(), "pkg")
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "extension.json"), []byte(
		`{"id":"dev.demo.mp","version":"1.0.0","api_version":"1","executable":"bin/mp","declarations":[{"kind":"demo"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"bin/mp-aix-ppc64", "bin/mp-plan9-386"} {
		if err := os.WriteFile(filepath.Join(src, rel), []byte("s"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	proj := t.TempDir()
	t.Chdir(proj)
	var buf bytes.Buffer
	err := cli.Run([]string{"extension", "install", src}, &buf)
	if err == nil || !strings.Contains(err.Error(), "aix/ppc64") {
		t.Fatalf("want an error naming available platforms, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(proj, ".mhl")); statErr == nil {
		t.Error("a failed multi-platform install must not create .mhl/")
	}
}

func TestExtensionListEmptyProject(t *testing.T) {
	t.Chdir(t.TempDir())
	var buf bytes.Buffer
	if err := cli.Run([]string{"extension", "list"}, &buf); err != nil {
		t.Fatalf("extension list: %v", err)
	}
	if !strings.Contains(buf.String(), "no external extensions locked") {
		t.Fatalf("unexpected output: %q", buf.String())
	}
}

func TestExtensionDoctorReportsABrokenLockEntry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	lock := `{"extensions":{"com.acme.ghost":{"version":"1.0.0","sha256":"abc"}}}`
	if err := os.MkdirAll(filepath.Join(dir, ".mhl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mhl", "extensions.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := cli.Run([]string{"extension", "doctor"}, &buf)
	if err == nil {
		t.Fatal("doctor should exit non-zero when an extension is broken")
	}
	out := buf.String()
	if !strings.Contains(out, "FAIL") || !strings.Contains(out, "com.acme.ghost") || !strings.Contains(out, "not installed") {
		t.Fatalf("doctor output missing the diagnosis: %q", out)
	}
}

func TestExtensionUnknownSubcommand(t *testing.T) {
	var buf bytes.Buffer
	if err := cli.Run([]string{"extension", "frobnicate"}, &buf); err == nil {
		t.Fatal("expected an error for an unknown extension subcommand")
	}
}

func TestExtensionInitScaffoldsAValidManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-crm")
	var buf bytes.Buffer
	if err := cli.Run([]string{"extension", "init", dir}, &buf); err != nil {
		t.Fatalf("extension init: %v", err)
	}
	for _, f := range []string{"extension.json", "declarations.json", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("init did not create %s", f)
		}
	}
	// The scaffolded manifest must at least parse as JSON with an id.
	raw, _ := os.ReadFile(filepath.Join(dir, "extension.json"))
	if !strings.Contains(string(raw), `"com.example.my-crm"`) {
		t.Fatalf("manifest missing derived id:\n%s", raw)
	}

	// Running init again into the now-populated dir must refuse, not clobber.
	var buf2 bytes.Buffer
	if err := cli.Run([]string{"extension", "init", dir}, &buf2); err == nil {
		t.Fatal("init into a populated directory should fail, not overwrite")
	}
}

func TestExtensionInstallVendorsAndPins(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)

	// A source extension dir with a manifest and a (non-runnable) executable.
	src := filepath.Join(t.TempDir(), "crm-src")
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"id": "com.acme.crm", "version": "2.0.0", "api_version": "1",
		"executable": "bin/crm",
		"declarations": [{ "kind": "crm" }]
	}`
	if err := os.WriteFile(filepath.Join(src, "extension.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "crm"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"extension", "install", src}, &buf); err != nil {
		t.Fatalf("extension install: %v\n%s", err, buf.String())
	}

	if _, err := os.Stat(filepath.Join(proj, ".mhl", "extensions", "com.acme.crm", "bin", "crm")); err != nil {
		t.Fatalf("executable not vendored: %v", err)
	}
	lock, err := os.ReadFile(filepath.Join(proj, ".mhl", "extensions.lock"))
	if err != nil {
		t.Fatalf("no lock written: %v", err)
	}
	if !strings.Contains(string(lock), `"com.acme.crm"`) || !strings.Contains(string(lock), `"2.0.0"`) || !strings.Contains(string(lock), `"sha256"`) {
		t.Fatalf("lock missing the pinned entry:\n%s", lock)
	}

	// And now `doctor` should be happy with it.
	var dbuf bytes.Buffer
	if err := cli.Run([]string{"extension", "doctor"}, &dbuf); err != nil {
		t.Fatalf("doctor after install: %v\n%s", err, dbuf.String())
	}
	if !strings.Contains(dbuf.String(), "ok") {
		t.Fatalf("doctor did not report the installed extension ok:\n%s", dbuf.String())
	}
}

// TestExtensionInstallFromGitRemote clones a local repo that carries the
// extension in a subdirectory, at a tagged ref, and checks the vendored tree
// plus the git provenance (source + resolved commit) written to the lock.
func TestExtensionInstallFromGitRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	// Build a repo whose ext/ holds a manifest + a stub executable, commit it
	// on a tag. This repo itself stands in for the "remote".
	remote := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = remote
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git("init", "-q")
	if err := os.MkdirAll(filepath.Join(remote, "ext", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"id": "com.acme.crm", "version": "3.1.0", "api_version": "1",
		"executable": "bin/crm",
		"declarations": [{ "kind": "crm" }]
	}`
	if err := os.WriteFile(filepath.Join(remote, "ext", "extension.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "ext", "bin", "crm"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "crm extension")
	git("tag", "v3.1.0")

	proj := t.TempDir()
	t.Chdir(proj)

	spec := "file://" + remote + "//ext#v3.1.0"
	var buf bytes.Buffer
	if err := cli.Run([]string{"extension", "install", spec}, &buf); err != nil {
		t.Fatalf("install from git: %v\n%s", err, buf.String())
	}

	if _, err := os.Stat(filepath.Join(proj, ".mhl", "extensions", "com.acme.crm", "bin", "crm")); err != nil {
		t.Fatalf("executable not vendored from the git subdir: %v", err)
	}
	lock, err := os.ReadFile(filepath.Join(proj, ".mhl", "extensions.lock"))
	if err != nil {
		t.Fatalf("no lock written: %v", err)
	}
	s := string(lock)
	if !strings.Contains(s, `"3.1.0"`) || !strings.Contains(s, `"sha256"`) {
		t.Fatalf("lock missing version/sha:\n%s", s)
	}
	if !strings.Contains(s, `"source": "`+spec+`"`) {
		t.Fatalf("lock missing the git source spec:\n%s", s)
	}
	if !strings.Contains(s, `"commit"`) {
		t.Fatalf("lock missing the resolved commit:\n%s", s)
	}

	// doctor is happy with a git-installed extension too.
	var dbuf bytes.Buffer
	if err := cli.Run([]string{"extension", "doctor"}, &dbuf); err != nil {
		t.Fatalf("doctor after git install: %v\n%s", err, dbuf.String())
	}
}

// TestExtensionInstallRejectsUnknownSource: a spec that is not a directory, a
// git URL, or an archive URL fails with a clear message rather than a
// confusing stat error.
func TestExtensionInstallRejectsUnknownSource(t *testing.T) {
	t.Chdir(t.TempDir())
	var buf bytes.Buffer
	err := cli.Run([]string{"extension", "install", "not-a-dir-or-url"}, &buf)
	if err == nil {
		t.Fatal("expected an error for an unresolvable install source")
	}
	if !strings.Contains(err.Error(), "not a directory, a git URL") || !strings.Contains(err.Error(), "archive URL") {
		t.Fatalf("unhelpful error: %v", err)
	}
}

// TestRunToleratesNoExtensionLock proves `mhl run` on a project with no
// external extensions is unaffected by the discovery step.
func TestRunToleratesNoExtensionLock(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	main := filepath.Join(dir, "main.mh")
	if err := os.WriteFile(main, []byte("pipeline P {\n step S {\n  log(\"hi\")\n }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := cli.Run([]string{"run", main}, &buf); err != nil {
		t.Fatalf("run: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "hi") {
		t.Fatalf("pipeline output missing: %q", buf.String())
	}
}
