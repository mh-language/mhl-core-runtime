package external

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// Manifest is a parsed extension.json. It is the static description of an
// installed external extension — read by lint and the LSP without ever
// spawning the process, and by the host to know what to run and what the
// extension is allowed to do.
type Manifest struct {
	// dir is the directory the manifest was loaded from; Executable is
	// resolved relative to it. manifestPath is the file itself.
	dir          string
	manifestPath string

	ID         string   `json:"id"`
	Version    string   `json:"version"`
	APIVersion string   `json:"api_version"`
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
	Env        []string `json:"env,omitempty"` // extra KEY=VALUE entries; the ambient environment is NOT inherited
	// Declares is the inline per-kind description. Provide this OR
	// DeclarationsFile, not both. `[{ "kind": "crm" }]` alone is enough —
	// properties/methods within each entry are optional ("debug symbols").
	Declares []extension.DeclarationSpec `json:"declarations,omitempty"`
	// DeclarationsFile is a path (relative to the manifest) to a JSON file
	// holding the declarations array — the "portable symbols" form, so the
	// SDK can regenerate the list without editing the manifest.
	DeclarationsFile string      `json:"declarations_file,omitempty"`
	Perms            Permissions `json:"permissions"`
}

// Permissions is the capability surface an extension declares it needs. The
// host uses it to gate secret resolution and (Bloc B, OS-level) network,
// subprocess and filesystem access.
type Permissions struct {
	Network    []string `json:"network,omitempty"`
	Secrets    []string `json:"secrets,omitempty"`
	Subprocess bool     `json:"subprocess,omitempty"`
	Filesystem []string `json:"filesystem,omitempty"`
}

// LoadManifest reads and validates the extension.json at path, pulling in a
// sidecar declarations file if the manifest names one.
func LoadManifest(path string) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("extension manifest %s: %w", path, err)
	}
	m.dir = filepath.Dir(path)
	m.manifestPath = path

	if m.DeclarationsFile != "" {
		if len(m.Declares) > 0 {
			return nil, fmt.Errorf("extension manifest %s: set \"declarations\" or \"declarations_file\", not both", path)
		}
		declPath := m.DeclarationsFile
		if !filepath.IsAbs(declPath) {
			declPath = filepath.Join(m.dir, declPath)
		}
		specs, err := loadDeclarationsFile(declPath)
		if err != nil {
			return nil, fmt.Errorf("extension manifest %s: %w", path, err)
		}
		m.Declares = specs
	}

	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("extension manifest %s: %w", path, err)
	}
	return &m, nil
}

// loadDeclarationsFile reads a sidecar holding the declarations. It accepts a
// bare array (`[{ "kind": ... }]`) or an object wrapping one
// (`{ "declarations": [ ... ] }`), so a file produced by `mhl extension
// package` and one hand-written both work.
func loadDeclarationsFile(path string) ([]extension.DeclarationSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("declarations_file: %w", err)
	}
	var specs []extension.DeclarationSpec
	if err := json.Unmarshal(raw, &specs); err == nil {
		return specs, nil
	}
	var wrapper struct {
		Declarations []extension.DeclarationSpec `json:"declarations"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("declarations_file %s: not a declarations array or { \"declarations\": [...] } object", path)
	}
	return wrapper.Declarations, nil
}

func (m *Manifest) validate() error {
	if m.ID == "" {
		return fmt.Errorf("missing \"id\"")
	}
	if m.APIVersion == "" {
		return fmt.Errorf("missing \"api_version\"")
	}
	if m.APIVersion != APIVersion {
		return fmt.Errorf("api_version %q is not supported by this runtime (wants %q)", m.APIVersion, APIVersion)
	}
	if m.Executable == "" {
		return fmt.Errorf("missing \"executable\"")
	}
	// Routing is the only hard requirement: at least one kind, each named.
	// Everything else in a declaration (properties, methods) is optional
	// tooling metadata.
	if len(m.Declares) == 0 {
		return fmt.Errorf("declares no kinds (need \"declarations\" or \"declarations_file\" with at least one { \"kind\": ... })")
	}
	seen := map[string]bool{}
	for i, d := range m.Declares {
		if d.Kind == "" {
			return fmt.Errorf("declarations[%d] has an empty kind", i)
		}
		if seen[d.Kind] {
			return fmt.Errorf("kind %q is declared twice", d.Kind)
		}
		seen[d.Kind] = true
	}
	return nil
}

// launchEnv is the environment the extension process runs with: two fixed
// vars plus whatever the manifest declares. The ambient environment is NOT
// inherited — no secret reaches the process except through secret.resolve.
func (m *Manifest) launchEnv() []string {
	env := []string{
		"MHL_EXTENSION_API=" + APIVersion,
		"MHL_EXTENSION_ID=" + m.ID,
	}
	return append(env, m.Env...)
}

// ExecutablePath is the absolute path to the extension binary for the running
// host. A manifest ships either a single "executable", or — for a
// multi-platform package — one file per platform named
// "<executable>-<goos>-<goarch>" ("…​.exe" on Windows) in the same directory.
// The plain name is tried first, the host-suffixed name second; if neither is
// on disk the plain path is returned anyway (the caller surfaces the open
// error), preserving the historical contract.
func (m *Manifest) ExecutablePath() string {
	if filepath.IsAbs(m.Executable) {
		return m.Executable
	}
	rel := m.executableRel(runtime.GOOS, runtime.GOARCH)
	if rel == "" {
		rel = m.Executable
	}
	return filepath.Join(m.dir, rel)
}

// executableRel returns the manifest-relative path to the binary for
// goos/goarch that actually exists on disk: the plain "executable", else the
// "<executable>-<goos>-<goarch>" convention. "" when neither is present. An
// absolute "executable", or a manifest with no directory context, is returned
// as given (the historical contract — nothing to look up).
func (m *Manifest) executableRel(goos, goarch string) string {
	if m.Executable == "" {
		return ""
	}
	if filepath.IsAbs(m.Executable) || m.dir == "" {
		return m.Executable
	}
	if _, err := os.Stat(filepath.Join(m.dir, m.Executable)); err == nil {
		return m.Executable
	}
	suffixed := m.Executable + "-" + goos + "-" + goarch
	if goos == "windows" {
		suffixed += ".exe"
	}
	if _, err := os.Stat(filepath.Join(m.dir, suffixed)); err == nil {
		return suffixed
	}
	return ""
}

// HostExecutableRel resolves the binary for the running host and returns its
// manifest-relative path, or an error naming the platforms the package does
// provide. Used by `mhl extension install` to vendor just the one binary.
func (m *Manifest) HostExecutableRel() (string, error) {
	if rel := m.executableRel(runtime.GOOS, runtime.GOARCH); rel != "" {
		return rel, nil
	}
	target := runtime.GOOS + "/" + runtime.GOARCH
	if got := m.availablePlatforms(); len(got) > 0 {
		return "", fmt.Errorf("no %s binary — this package provides: %s", target, strings.Join(got, ", "))
	}
	return "", fmt.Errorf("no %s binary at %q", target, m.Executable)
}

// availablePlatforms lists the "<goos>/<goarch>" a multi-platform package
// ships, discovered from the "<executable>-*" files next to the manifest.
func (m *Manifest) availablePlatforms() []string {
	if m.Executable == "" || m.dir == "" {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(m.dir, m.Executable+"-*"))
	var out []string
	for _, p := range matches {
		tail := strings.TrimPrefix(filepath.Base(p), filepath.Base(m.Executable)+"-")
		tail = strings.TrimSuffix(tail, ".exe")
		if i := strings.LastIndex(tail, "-"); i > 0 {
			out = append(out, tail[:i]+"/"+tail[i+1:])
		}
	}
	sort.Strings(out)
	return out
}

// AllowsSecret reports whether the manifest permits resolving ref. An empty
// Secrets list means no secret access; "*" means any.
func (m *Manifest) AllowsSecret(ref string) bool {
	for _, allowed := range m.Perms.Secrets {
		if allowed == "*" || allowed == ref {
			return true
		}
	}
	return false
}
