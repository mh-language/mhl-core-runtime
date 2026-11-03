package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
	"github.com/mh-language/mhl-core-runtime/internal/pack"
)

// runPack implements:
//
//	mhl pack <dir> -o <file> [--new-key]
//
// It seals every file under dir into an encrypted workflows pack (DMPK v2,
// internal/pack) for `mhl serve mcp --http --workflows-pack`. The key comes
// from MHL_SERVE_WORKFLOWS_KEY — never a flag, argv is readable by every
// process — or, with --new-key, is generated and printed to out as the only
// line there, so `KEY=$(mhl pack ... --new-key)` captures it; the summary
// then goes to stderr instead.
//
// Hidden entries (any path element starting with ".", e.g. .mhl/, .git) and
// the output file itself are left out; a symlink is an error rather than a
// silently missing file. Entries are sorted by path. Before anything is
// written, the sealed bytes are opened again and the workflows loaded from
// them, so a pack that would not serve is never produced.
func runPack(args []string, out io.Writer) error {
	var dir, output string
	newKey := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--output":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a file argument", args[i])
			}
			i++
			output = args[i]
		case "--new-key":
			newKey = true
		default:
			if strings.HasPrefix(args[i], "-") || dir != "" {
				return fmt.Errorf("unexpected argument %q", args[i])
			}
			dir = args[i]
		}
	}
	if dir == "" || output == "" {
		return fmt.Errorf("usage: mhl pack <dir> -o <file> [--new-key]")
	}

	envKey := strings.TrimSpace(os.Getenv(workflowsKeyEnv))
	var key []byte
	defer func() { clear(key) }()
	switch {
	case newKey && envKey != "":
		return fmt.Errorf("--new-key conflicts with %s being set", workflowsKeyEnv)
	case newKey:
		key = make([]byte, pack.KeySize)
		if _, err := rand.Read(key); err != nil {
			return err
		}
	case envKey == "":
		return fmt.Errorf("no key: set %s (64 hex characters) or pass --new-key", workflowsKeyEnv)
	default:
		k, err := hex.DecodeString(envKey)
		if err != nil || len(k) != pack.KeySize {
			clear(k)
			return fmt.Errorf("%s: want %d hex characters", workflowsKeyEnv, 2*pack.KeySize)
		}
		key = k
	}

	files, err := collectPackFiles(dir, output)
	if err != nil {
		return err
	}
	raw, err := pack.Seal(files, key)
	if err != nil {
		return err
	}
	workflows, err := verifyPack(dir, raw, key)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(output, raw); err != nil {
		return err
	}

	summary := out
	if newKey {
		fmt.Fprintln(out, hex.EncodeToString(key))
		summary = os.Stderr
	}
	fmt.Fprintf(summary, "packed %d file(s), %d workflow(s) (%s) into %s\n",
		len(files), len(workflows), strings.Join(workflows, ", "), output)
	return nil
}

// collectPackFiles reads every regular file under dir, skipping hidden
// entries and output, as slash-separated paths relative to dir, sorted.
func collectPackFiles(dir, output string) ([]pack.File, error) {
	outAbs, err := filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	var files []pack.File
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlinks are not packed", p)
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if abs, err := filepath.Abs(p); err == nil && abs == outAbs {
			return nil
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, pack.File{Path: filepath.ToSlash(rel), Content: content})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// verifyPack opens raw with key and loads its workflows exactly as `serve
// mcp --workflows-pack` would, returning their sorted names.
func verifyPack(dir string, raw, key []byte) ([]string, error) {
	opened, err := pack.Open(raw, key)
	if err != nil {
		return nil, err
	}
	fsys, err := pack.NewFS(opened)
	if err != nil {
		return nil, err
	}
	parsed, err := execsvc.ParseFS(dir, fsys)
	if err != nil {
		return nil, err
	}
	loaded, err := execsvc.LoadFilesFS(dir, fsys, parsed)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(loaded))
	for n := range loaded {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// writeFileAtomic writes data to path (mode 0600) through a temp file in the
// same directory, so a failed run never leaves a truncated pack behind.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mhl-pack-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
