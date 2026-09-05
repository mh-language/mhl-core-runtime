package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/engine/interpreter"
	"github.com/mh-language/mhl-core-runtime/internal/extension/external"
)

// runExtension implements the `mhl extension` sub-surface:
//
//	list           — every entry in .mhl/extensions.lock and its status
//	doctor         — validate each; non-zero exit if any is broken
//	init <dir>     — scaffold an extension project (manifest + sidecar + README)
//	test <dir>     — spawn the extension and smoke-test the protocol
//	package <dir>  — refresh the declarations sidecar from the running extension
//	install <src>  — vendor an extension into .mhl/extensions/ and pin it; src
//	                 is a local directory, a git remote
//	                 (<url>[//<subdir>][#<ref>]), or a published archive URL
//	                 (….tar.gz / .zip[#sha256=<hex>])
//
// `install` from a remote still only *vendors and pins* — the executable is
// hashed into .mhl/extensions.lock and never run, so the run-time trust check
// (a swapped binary is refused) is unchanged. An archive's optional
// #sha256=<hex> is verified against the downloaded bytes before extraction.
func runExtension(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl extension <list|doctor|init|test|package|install> [args]")
	}
	switch args[0] {
	case "list":
		return extensionList(out)
	case "doctor":
		return extensionDoctor(out)
	case "init":
		return extensionInit(args[1:], out)
	case "test":
		return extensionTest(args[1:], out)
	case "package":
		return extensionPackage(args[1:], out)
	case "install":
		return extensionInstall(args[1:], out)
	default:
		return fmt.Errorf("unknown extension command %q", args[0])
	}
}

func manifestArg(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("expected an extension directory")
	}
	p, ok := external.FindManifestFile(args[0])
	if !ok {
		return "", fmt.Errorf("no extension.json or extension.mh in %s", args[0])
	}
	return p, nil
}

func extensionInit(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl extension init <dir>")
	}
	dir := args[0]

	// Never scaffold over existing files. `init` is for a fresh project dir;
	// pointing it at a populated one (or `.`) must fail, not overwrite.
	for _, f := range []string{"extension.json", "extension.mh", "declarations.json", "declarations.mh", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return fmt.Errorf("%s already exists in %s — refusing to overwrite; use an empty directory", f, dir)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	base := filepath.Base(dir)
	if base == "." || base == string(filepath.Separator) || base == "" {
		abs, _ := filepath.Abs(dir)
		base = filepath.Base(abs)
	}
	id := "com.example." + strings.ToLower(base)

	manifest := map[string]any{
		"id":                id,
		"version":           "0.1.0",
		"api_version":       external.APIVersion,
		"executable":        "bin/" + base,
		"declarations_file": "declarations.json",
		"permissions":       map[string]any{"secrets": []string{}},
	}
	decls := []map[string]any{{
		"kind": "example",
		"methods": []map[string]any{{
			"name":          "ping",
			"signature":     "ping() -> string",
			"documentation": "Replace with your extension's real methods.",
		}},
	}}

	if err := writeJSON(filepath.Join(dir, "extension.json"), manifest); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "declarations.json"), decls); err != nil {
		return err
	}
	readme := "# " + id + "\n\nAn mhl external extension. Build your executable to `" +
		manifest["executable"].(string) + "` — it must speak the newline-delimited JSON-RPC\n" +
		"protocol on stdin/stdout (see docs/extension-protocol.md). Then:\n\n" +
		"    mhl extension test .        # smoke-test the protocol\n" +
		"    mhl extension package .     # refresh declarations.json from the running extension\n" +
		"    mhl extension install .     # vendor it into a project's .mhl/extensions/\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(out, "scaffolded %s in %s\n", id, dir)
	return nil
}

func extensionTest(args []string, out io.Writer) error {
	path, err := manifestArg(args)
	if err != nil {
		return err
	}
	m, err := external.LoadManifest(path)
	if err != nil {
		return err
	}
	results, err := external.Smoke(m)
	if err != nil {
		return err
	}
	failed := 0
	for _, r := range results {
		mark := "ok  "
		if !r.OK {
			mark = "FAIL"
			failed++
		}
		fmt.Fprintf(out, "%s  %s.%s — %s\n", mark, r.Kind, r.Method, r.Detail)
	}
	if failed > 0 {
		return fmt.Errorf("%d method(s) failed the smoke test", failed)
	}
	return nil
}

func extensionPackage(args []string, out io.Writer) error {
	path, err := manifestArg(args)
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Ext(path), ".mh") {
		return fmt.Errorf("%s declares its capabilities inline (\"properties\"/methods in the \"extensible\" block) — there is no separate declarations sidecar to refresh; edit it directly", path)
	}
	m, err := external.LoadManifest(path)
	if err != nil {
		return err
	}
	specs, err := external.Describe(m)
	if err != nil {
		return fmt.Errorf("describing the extension: %w", err)
	}
	if len(specs) == 0 {
		fmt.Fprintln(out, "the extension reported no declarations in its handshake — nothing to refresh")
		return nil
	}
	sidecar := m.DeclarationsFile
	if sidecar == "" {
		sidecar = "declarations.json"
	}
	dest := filepath.Join(filepath.Dir(path), sidecar)
	if err := writeJSON(dest, specs); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %d kind(s) to %s\n", len(specs), dest)
	if m.DeclarationsFile == "" {
		fmt.Fprintf(out, "add \"declarations_file\": %q to extension.json (and remove any inline \"declarations\")\n", sidecar)
	}
	return nil
}

func extensionInstall(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl extension install <dir | git-url[//subdir][#ref] | archive-url(.tar.gz|.zip)[#sha256=hex]>")
	}
	spec := args[0]

	// A local directory always wins. Otherwise the spec is a remote: an
	// archive URL (what `make release` publishes) or a git URL. Archive is
	// checked first — a `.tar.gz` URL also satisfies the git-URL shape.
	src := spec
	var srcRef, srcCommit string
	if fi, err := os.Stat(spec); err != nil || !fi.IsDir() {
		switch as, ok := parseArchiveSource(spec); {
		case ok:
			dir, cleanup, ferr := fetchArchiveSource(context.Background(), as, out)
			if ferr != nil {
				return ferr
			}
			defer cleanup()
			src, srcRef = dir, as.Raw
		default:
			gs, gok := parseGitSource(spec)
			if !gok {
				return fmt.Errorf("%q is not a directory, a git URL (<url>[//subdir][#ref]), or an archive URL (….tar.gz / .zip[#sha256=hex])", spec)
			}
			dir, commit, cleanup, ferr := fetchGitSource(context.Background(), gs, out)
			if ferr != nil {
				return ferr
			}
			defer cleanup()
			src, srcRef, srcCommit = dir, gs.Raw, commit
		}
	}

	srcManifest, ok := external.FindManifestFile(src)
	if !ok {
		return fmt.Errorf("no extension.json or extension.mh in %s", src)
	}
	m, err := external.LoadManifest(srcManifest)
	if err != nil {
		return err
	}

	// Resolve the binary for this host up front: it fails clearly ("no
	// darwin/arm64 binary — this package provides: …") before anything is
	// written, and tells us whether the source is a multi-platform package.
	exeRel, err := m.HostExecutableRel()
	if err != nil {
		return err
	}

	dest := filepath.Join(".mhl", "extensions", m.ID)
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if exeRel == m.Executable {
		// Single-platform (or a dev dir): copy it all, as before.
		if err := copyTree(src, dest); err != nil {
			return fmt.Errorf("copying into %s: %w", dest, err)
		}
	} else {
		// Multi-platform package (bin/<name>-<goos>-<goarch>): vendor only
		// this host's binary plus the metadata the runtime reads.
		if err := installHostBinary(src, dest, m, exeRel); err != nil {
			return fmt.Errorf("installing into %s: %w", dest, err)
		}
		fmt.Fprintf(out, "selected %s/%s binary: %s\n", runtime.GOOS, runtime.GOARCH, filepath.Base(exeRel))
	}

	destManifest, ok := external.FindManifestFile(dest)
	if !ok {
		return fmt.Errorf("no extension.json or extension.mh in %s after install", dest)
	}
	installed, err := external.LoadManifest(destManifest)
	if err != nil {
		return err
	}
	sum, err := external.HashExecutable(installed)
	if err != nil {
		return err
	}

	lockPath := external.LockPath
	lock, err := external.LoadLock(lockPath)
	if err != nil {
		return err
	}
	lock.Extensions[m.ID] = external.LockEntry{
		Version: m.Version, SHA256: sum, Source: srcRef, Commit: srcCommit,
	}
	if err := lock.Save(lockPath); err != nil {
		return err
	}

	fmt.Fprintf(out, "installed %s %s -> %s\n", m.ID, m.Version, dest)
	switch {
	case srcRef != "" && srcCommit != "":
		fmt.Fprintf(out, "from %s (commit %s)\n", srcRef, srcCommit)
	case srcRef != "":
		fmt.Fprintf(out, "from %s\n", srcRef)
	}
	fmt.Fprintf(out, "pinned in %s (sha256 %s…)\n", lockPath, sum[:12])
	return nil
}

func writeJSON(path string, v any) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

// copyFile copies one file, creating parent dirs and preserving the mode bits
// (an executable stays executable).
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode().Perm())
}

// installHostBinary vendors a multi-platform package (bin/<name>-<goos>-<goarch>)
// as a single-platform install: only the resolved host binary (exeRel) plus
// the metadata files the runtime and `mhl extension doctor` read. The
// extension.json is copied unchanged — its "executable" stem plus the
// "<goos>-<goarch>" convention still resolves exeRel on this host.
func installHostBinary(src, dst string, m *external.Manifest, exeRel string) error {
	if err := copyFile(filepath.Join(src, exeRel), filepath.Join(dst, exeRel)); err != nil {
		return err
	}
	meta := []string{"extension.json", "extension.mh", "declarations.json", "declarations.mh", "README.md"}
	if m.DeclarationsFile != "" {
		meta = append(meta, m.DeclarationsFile)
	}
	for _, name := range meta {
		s := filepath.Join(src, name)
		if _, err := os.Stat(s); err != nil {
			continue // declarations.json / README.md are optional
		}
		if err := copyFile(s, filepath.Join(dst, name)); err != nil {
			return err
		}
	}
	return nil
}

func extensionList(out io.Writer) error {
	statuses, err := external.Inspect(".")
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		fmt.Fprintf(out, "no external extensions locked (%s absent or empty)\n", external.LockPath)
		return nil
	}
	for _, s := range statuses {
		mark := "ok"
		if !s.OK {
			mark = "BROKEN"
		}
		fmt.Fprintf(out, "%-24s %-10s [%s] %s\n", s.ID, s.Version, strings.Join(s.Kinds, ","), mark)
		if !s.OK {
			fmt.Fprintf(out, "    %s\n", s.Problem)
		}
	}
	return nil
}

func extensionDoctor(out io.Writer) error {
	statuses, err := external.Inspect(".")
	if err != nil {
		return err
	}
	broken := 0
	for _, s := range statuses {
		if s.OK {
			fmt.Fprintf(out, "ok    %s %s\n", s.ID, s.Version)
			continue
		}
		broken++
		fmt.Fprintf(out, "FAIL  %s: %s\n", s.ID, s.Problem)
	}
	if broken > 0 {
		return fmt.Errorf("%d extension(s) failed doctor", broken)
	}
	if len(statuses) == 0 {
		fmt.Fprintln(out, "no external extensions locked — nothing to check")
	}
	return nil
}

// loadSessionExtensions discovers the project's external extensions, installs
// them as the interpreter's session provider, and returns a cleanup that
// clears the provider and shuts every extension process down. Discovery
// problems are reported to out but never abort the run.
func loadSessionExtensions(out io.Writer) func() {
	set, err := external.Discover(".")
	if err != nil {
		fmt.Fprintf(out, "warning: reading %s: %v\n", external.LockPath, err)
		return func() {}
	}
	for _, p := range set.Problems() {
		fmt.Fprintf(out, "warning: extension %q not loaded: %s\n", p.ID, p.Message)
	}
	interpreter.SetSessionExtensions(set.Extensions)
	return func() {
		interpreter.SetSessionExtensions(nil)
		set.CloseAll()
	}
}
