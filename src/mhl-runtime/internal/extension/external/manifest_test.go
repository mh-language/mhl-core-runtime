package external

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestExecutableResolvesByPlatformConvention: a package that ships one binary
// per platform as "<executable>-<goos>-<goarch>" resolves the host's; a plain
// "executable" file always wins; nothing on disk for the host is a clear
// error naming what the package does provide.
func TestExecutableResolvesByPlatformConvention(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// One binary per platform, no plain "bin/x".
	for _, p := range []string{"bin/x-linux-amd64", "bin/x-linux-arm64", "bin/x-windows-amd64.exe"} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte("stub"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Add one for the running host so HostExecutableRel succeeds here.
	hostBin := "bin/x-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		hostBin += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, hostBin), []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mp := writeManifest(t, dir, `{"id":"x","api_version":"1","executable":"bin/x","declarations":[{"kind":"k"}]}`)
	m, err := LoadManifest(mp)
	if err != nil {
		t.Fatal(err)
	}

	if got := m.executableRel("linux", "amd64"); got != "bin/x-linux-amd64" {
		t.Errorf("linux/amd64 -> %q, want bin/x-linux-amd64", got)
	}
	if got := m.executableRel("windows", "amd64"); got != "bin/x-windows-amd64.exe" {
		t.Errorf("windows/amd64 -> %q, want …-windows-amd64.exe", got)
	}
	if got := m.executableRel("plan9", "mips"); got != "" {
		t.Errorf("unsupported platform -> %q, want \"\"", got)
	}
	rel, err := m.HostExecutableRel()
	if err != nil || rel != hostBin {
		t.Fatalf("HostExecutableRel = %q, %v; want %q, nil", rel, err, hostBin)
	}
	if !strings.HasSuffix(m.ExecutablePath(), hostBin) {
		t.Errorf("ExecutablePath = %q, want …/%s", m.ExecutablePath(), hostBin)
	}

	// A plain bin/x present -> it wins regardless of platform.
	if err := os.WriteFile(filepath.Join(dir, "bin/x"), []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	m2, _ := LoadManifest(mp)
	if got := m2.executableRel("linux", "amd64"); got != "bin/x" {
		t.Errorf("plain executable should win, got %q", got)
	}
}

// TestHostExecutableRelListsAvailablePlatforms: the error when the host is
// unsupported names the platforms that are.
func TestHostExecutableRelListsAvailablePlatforms(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Only two, neither for a plausible host arch string used below.
	for _, p := range []string{"bin/x-aix-ppc64", "bin/x-solaris-amd64"} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte("s"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m, err := LoadManifest(writeManifest(t, dir, `{"id":"x","api_version":"1","executable":"bin/x","declarations":[{"kind":"k"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.HostExecutableRel()
	if err == nil || !strings.Contains(err.Error(), "aix/ppc64") || !strings.Contains(err.Error(), "solaris/amd64") {
		t.Fatalf("error should list available platforms, got: %v", err)
	}
}

func writeManifest(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "extension.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestManifestMinimalDeclarationIsValid: routing is the only requirement —
// properties and methods are optional tooling metadata.
func TestManifestMinimalDeclarationIsValid(t *testing.T) {
	dir := t.TempDir()
	p := writeManifest(t, dir, `{
		"id": "com.acme.crm",
		"api_version": "1",
		"executable": "bin/crm",
		"declarations": [{ "kind": "crm" }]
	}`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("a kind-only declaration should be valid: %v", err)
	}
	if len(m.Declares) != 1 || m.Declares[0].Kind != "crm" {
		t.Fatalf("unexpected declares: %+v", m.Declares)
	}
	if len(m.Declares[0].Methods) != 0 {
		t.Fatalf("expected no methods, got %+v", m.Declares[0].Methods)
	}
}

func TestManifestDeclarationsFileSidecar(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "crm.d.json"), []byte(`[
		{ "kind": "crm", "methods": [{ "name": "lookup", "signature": "lookup(id: string) -> object" }] }
	]`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := writeManifest(t, dir, `{
		"id": "com.acme.crm",
		"api_version": "1",
		"executable": "bin/crm",
		"declarations_file": "crm.d.json"
	}`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Declares) != 1 || len(m.Declares[0].Methods) != 1 || m.Declares[0].Methods[0].Name != "lookup" {
		t.Fatalf("sidecar declarations not loaded: %+v", m.Declares)
	}
}

func TestManifestDeclarationsFileWrappedForm(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "d.json"), []byte(`{ "declarations": [{ "kind": "crm" }] }`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations_file": "d.json"
	}`)
	if _, err := LoadManifest(p); err != nil {
		t.Fatalf("wrapped { \"declarations\": [...] } form should load: %v", err)
	}
}

func TestManifestDeclarationsFileMHLSidecar(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "crm.d.mh"), []byte(`[
		{ kind: "crm", methods: [{ name: "lookup", signature: "lookup(id: string) -> object" }] }
	]`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := writeManifest(t, dir, `{
		"id": "com.acme.crm",
		"api_version": "1",
		"executable": "bin/crm",
		"declarations_file": "crm.d.mh"
	}`)
	m, err := LoadManifest(p)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Declares) != 1 || len(m.Declares[0].Methods) != 1 || m.Declares[0].Methods[0].Name != "lookup" {
		t.Fatalf("mhl sidecar declarations not loaded: %+v", m.Declares)
	}
}

func TestManifestDeclarationsFileMHLWrappedForm(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "d.mh"), []byte(`{ declarations: [{ kind: "crm" }] }`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations_file": "d.mh"
	}`)
	if _, err := LoadManifest(p); err != nil {
		t.Fatalf("wrapped { declarations: [...] } mhl form should load: %v", err)
	}
}

func TestManifestDeclarationsFileMHLRejectsBadLiteral(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "d.mh"), []byte(`{ declarations: someIdent }`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations_file": "d.mh"
	}`)
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected an error for a non-literal declarations_file")
	}
}

func TestManifestRejectsBothInlineAndFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "d.json"), []byte(`[{ "kind": "crm" }]`), 0o644)
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations": [{ "kind": "crm" }],
		"declarations_file": "d.json"
	}`)
	_, err := LoadManifest(p)
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("expected a both-forms error, got: %v", err)
	}
}

func TestManifestMissingSidecarIsAnError(t *testing.T) {
	dir := t.TempDir()
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations_file": "nope.json"
	}`)
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("expected an error for a missing declarations_file")
	}
}

func TestManifestRejectsDuplicateKind(t *testing.T) {
	dir := t.TempDir()
	p := writeManifest(t, dir, `{
		"id": "x", "api_version": "1", "executable": "bin/x",
		"declarations": [{ "kind": "crm" }, { "kind": "crm" }]
	}`)
	if _, err := LoadManifest(p); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("expected a duplicate-kind error, got: %v", err)
	}
}
