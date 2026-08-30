package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

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
