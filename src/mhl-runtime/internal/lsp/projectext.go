package lsp

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
	"github.com/mh-language/mhl-core-runtime/internal/extension/external"
)

// findProjectRoot walks upward from filePath's directory looking for a
// .mhl/extensions.lock — the file `mhl extension install` writes — stopping
// at the first ancestor that has one, or at the filesystem root ("", false).
// This mirrors how the interpreter and CLI locate a project's extensions
// (external.LockPath, relative to a project dir), except the LSP is never
// told a project dir up front, only the path of whatever buffer is open.
func findProjectRoot(filePath string) (string, bool) {
	dir := filepath.Dir(filePath)
	for {
		if _, err := os.Stat(filepath.Join(dir, external.LockPath)); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// projectExtensionSpec looks up kind among filePath's project's locked
// external extensions — the ones `mhl extension install` vendored under
// .mhl/extensions/<id>/ (or the user-global ~/.mhl/extensions/<id>/) —
// reading whichever manifest form that id was installed as (extension.json
// or extension.mh). With a provider (`from "<id>"`) only that id is read;
// without one, kind must be declared by exactly one locked extension, the
// same rule the runtime's Registry.Lookup applies. It never spawns the
// extension's process and never checks its executable's pinned sha256: those
// are run-time trust concerns (external.Discover's job); this is read-only
// metadata for completion and signature help, the same spirit as
// extension.BuiltinSpec for a built-in kind. ok is false when filePath isn't
// under a locked project, no locked extension (or not provider) declares
// kind, kind is ambiguous, or a manifest fails to load.
func projectExtensionSpec(filePath, kind, provider string) (extension.DeclarationSpec, bool) {
	root, ok := findProjectRoot(filePath)
	if !ok {
		return extension.DeclarationSpec{}, false
	}
	lock, err := external.LoadLock(filepath.Join(root, external.LockPath))
	if err != nil {
		return extension.DeclarationSpec{}, false
	}
	home, _ := os.UserHomeDir()
	var (
		found   extension.DeclarationSpec
		matches int
	)
	for id := range lock.Extensions {
		if provider != "" && id != provider {
			continue
		}
		if spec, ok := lockedSpec(root, home, id, kind); ok {
			found = spec
			matches++
		}
	}
	return found, matches == 1
}

// lockedSpec reads id's installed manifest (project dir first, then the
// user-global one) and returns its spec for kind.
func lockedSpec(root, home, id, kind string) (extension.DeclarationSpec, bool) {
	dirs := []string{filepath.Join(root, ".mhl", "extensions", id)}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".mhl", "extensions", id))
	}
	for _, dir := range dirs {
		manifestPath, ok := external.FindManifestFile(dir)
		if !ok {
			continue
		}
		m, err := external.LoadManifest(manifestPath)
		if err != nil {
			return extension.DeclarationSpec{}, false
		}
		for _, d := range m.Declares {
			if d.Kind == kind {
				return d, true
			}
		}
		// This id's manifest was found and doesn't declare kind; the other
		// search dir is a different install location for the same id.
		return extension.DeclarationSpec{}, false
	}
	return extension.DeclarationSpec{}, false
}

// providerInfo is one locked external extension able to serve a kind — a
// candidate for an `extension <kind> <Name> from "<id>"` clause.
type providerInfo struct {
	ID      string
	Version string
}

// projectProviders lists, sorted by id, every extension locked in filePath's
// project whose installed manifest declares kind. nil when filePath isn't
// under a locked project or none does.
func projectProviders(filePath, kind string) []providerInfo {
	root, ok := findProjectRoot(filePath)
	if !ok {
		return nil
	}
	lock, err := external.LoadLock(filepath.Join(root, external.LockPath))
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	var out []providerInfo
	for id, entry := range lock.Extensions {
		if _, ok := lockedSpec(root, home, id, kind); ok {
			out = append(out, providerInfo{ID: id, Version: entry.Version})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
