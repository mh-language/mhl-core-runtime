package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/extension/external"
)

// maxArchiveBytes caps a downloaded extension archive (and any single file
// inside it) so a hostile or truncated URL cannot exhaust memory or disk.
const maxArchiveBytes = 256 << 20 // 256 MiB

// archiveSource is a parsed `mhl extension install` argument that points at a
// packaged extension archive over HTTP — the shape `make release` produces —
// rather than a directory or a git remote:
//
//	https://host/path/mhl-store-s3_linux_amd64.tar.gz[#sha256=<hex>]
//
// `.tar.gz` / `.tgz` and `.zip` are recognised. An optional `#sha256=<hex>`
// fragment is checked against the downloaded bytes before extraction. Raw is
// the whole spec, recorded verbatim in the lock's "source".
type archiveSource struct {
	URL    string
	SHA256 string // optional, lowercase hex
	Raw    string
	isZip  bool
}

// parseArchiveSource reports whether spec is an HTTP(S) URL naming a `.tar.gz`
// / `.tgz` / `.zip`, splitting off an optional `#sha256=` fragment. Detection
// is purely lexical — the caller checks for a local directory first.
func parseArchiveSource(spec string) (archiveSource, bool) {
	as := archiveSource{Raw: spec}
	s := spec
	if i := strings.LastIndex(s, "#"); i >= 0 {
		frag := s[i+1:]
		s = s[:i]
		v, ok := strings.CutPrefix(frag, "sha256=")
		if !ok {
			return archiveSource{}, false
		}
		as.SHA256 = strings.ToLower(strings.TrimSpace(v))
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return archiveSource{}, false
	}
	switch low := strings.ToLower(s); {
	case strings.HasSuffix(low, ".tar.gz"), strings.HasSuffix(low, ".tgz"):
		as.isZip = false
	case strings.HasSuffix(low, ".zip"):
		as.isZip = true
	default:
		return archiveSource{}, false
	}
	as.URL = s
	return as, true
}

// fetchArchiveSource downloads as.URL, verifies its sha256 when one was given,
// extracts it into a fresh temp directory, and returns the directory that
// holds extension.json (the archive root, or its single top-level directory)
// plus a cleanup the caller must defer.
func fetchArchiveSource(ctx context.Context, as archiveSource, out io.Writer) (dir string, cleanup func(), err error) {
	fmt.Fprintf(out, "downloading %s\n", as.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, as.URL, nil)
	if err != nil {
		return "", func() {}, err
	}
	req.Header.Set("User-Agent", "mhl-extension-install")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return "", func() {}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", func() {}, fmt.Errorf("GET %s: HTTP %d", as.URL, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return "", func() {}, err
	}
	if len(data) > maxArchiveBytes {
		return "", func() {}, fmt.Errorf("archive exceeds %d bytes", maxArchiveBytes)
	}
	if as.SHA256 != "" {
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != as.SHA256 {
			return "", func() {}, fmt.Errorf("archive sha256 mismatch: got %s, expected %s", got, as.SHA256)
		}
		fmt.Fprintln(out, "sha256 verified")
	}

	tmp, err := os.MkdirTemp("", "mhl-ext-archive-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }

	if as.isZip {
		err = extractZip(data, tmp)
	} else {
		err = extractTarGz(data, tmp)
	}
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("extracting %s: %w", as.URL, err)
	}

	root, err := manifestRoot(tmp)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return root, cleanup, nil
}

// manifestRoot returns the directory under tmp that holds a manifest
// (extension.json or extension.mh): tmp itself, or its single top-level
// subdirectory (an archive made with `tar czf x.tgz <name>/` wraps
// everything in one directory).
func manifestRoot(tmp string) (string, error) {
	if _, ok := external.FindManifestFile(tmp); ok {
		return tmp, nil
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 1 {
		inner := filepath.Join(tmp, dirs[0])
		if _, ok := external.FindManifestFile(inner); ok {
			return inner, nil
		}
	}
	return "", fmt.Errorf("archive has no extension.json or extension.mh at its root or in a single top-level directory")
}

// safeJoin resolves an archive entry name under base, rejecting any path that
// would escape it (absolute, or `..` reaching above base). It is the guard
// against the classic zip/tar traversal attack.
func safeJoin(base, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	sep := string(filepath.Separator)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+sep) {
		return "", fmt.Errorf("entry %q escapes the extraction directory", name)
	}
	joined := filepath.Join(base, clean)
	if joined != base && !strings.HasPrefix(joined, base+sep) {
		return "", fmt.Errorf("entry %q escapes the extraction directory", name)
	}
	return joined, nil
}

func extractTarGz(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if hdr.Size > maxArchiveBytes {
				return fmt.Errorf("entry %q exceeds %d bytes", hdr.Name, maxArchiveBytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode).Perm())
			if err != nil {
				return err
			}
			_, cErr := io.CopyN(f, tr, hdr.Size)
			f.Close()
			if cErr != nil {
				return cErr
			}
		default:
			// Symlinks/hardlinks/devices have no place in an extension package
			// and are an escape vector — skip them.
		}
	}
}

func extractZip(data []byte, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, zf := range zr.File {
		target, err := safeJoin(dest, zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode().Perm())
		if err != nil {
			rc.Close()
			return err
		}
		_, cErr := io.Copy(f, io.LimitReader(rc, maxArchiveBytes+1))
		f.Close()
		rc.Close()
		if cErr != nil {
			return cErr
		}
	}
	return nil
}
