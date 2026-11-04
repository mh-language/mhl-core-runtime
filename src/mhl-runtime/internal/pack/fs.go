package pack

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// FS is a read-only, in-memory fs.FS over a pack's files. Directories are
// synthesized from the file paths. It is what the workflow loader reads a
// pack through, so the loader itself stays testable with any fs.FS.
type FS struct {
	files map[string][]byte
	// dirs maps every directory (the root is ".") to its sorted entries.
	dirs map[string][]fs.DirEntry
}

// NewFS indexes files. A path that is both a file and the parent of another
// path is an error.
func NewFS(files []File) (*FS, error) {
	f := &FS{files: make(map[string][]byte, len(files)), dirs: map[string][]fs.DirEntry{".": nil}}
	for _, file := range files {
		if err := validPath(file.Path); err != nil {
			return nil, fmt.Errorf("pack: %w", err)
		}
		if _, dup := f.files[file.Path]; dup {
			return nil, fmt.Errorf("pack: repeated path %q", file.Path)
		}
		f.files[file.Path] = file.Content
	}
	for name, content := range f.files {
		child := entry{name: path.Base(name), size: int64(len(content))}
		for dir := path.Dir(name); ; dir = path.Dir(dir) {
			if _, isFile := f.files[dir]; isFile {
				return nil, fmt.Errorf("pack: %q is both a file and a directory", dir)
			}
			_, known := f.dirs[dir]
			f.dirs[dir] = append(f.dirs[dir], child)
			if known || dir == "." {
				break
			}
			child = entry{name: path.Base(dir), dir: true}
		}
	}
	for _, entries := range f.dirs {
		slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	}
	return f, nil
}

// Open implements fs.FS.
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if content, ok := f.files[name]; ok {
		return &openFile{entry: entry{name: path.Base(name), size: int64(len(content))}, r: bytes.NewReader(content)}, nil
	}
	if entries, ok := f.dirs[name]; ok {
		return &openDir{entry: entry{name: path.Base(name), dir: true}, entries: entries}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadFile implements fs.ReadFileFS. The returned slice is a copy.
func (f *FS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
	}
	content, ok := f.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	return bytes.Clone(content), nil
}

// ReadDir implements fs.ReadDirFS.
func (f *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	entries, ok := f.dirs[name]
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return slices.Clone(entries), nil
}

// entry is both the fs.FileInfo and the fs.DirEntry of one path.
type entry struct {
	name string
	size int64
	dir  bool
}

func (e entry) Name() string               { return e.name }
func (e entry) Size() int64                { return e.size }
func (e entry) ModTime() time.Time         { return time.Time{} }
func (e entry) IsDir() bool                { return e.dir }
func (e entry) Sys() any                   { return nil }
func (e entry) Info() (fs.FileInfo, error) { return e, nil }
func (e entry) Type() fs.FileMode          { return e.Mode().Type() }
func (e entry) Mode() fs.FileMode {
	if e.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

type openFile struct {
	entry
	r *bytes.Reader
}

func (o *openFile) Stat() (fs.FileInfo, error) { return o.entry, nil }
func (o *openFile) Read(b []byte) (int, error) { return o.r.Read(b) }
func (o *openFile) Close() error               { return nil }

// Seek and ReadAt let fstest.TestFS (and any io.ReadSeeker caller) treat it
// like an os.File.
func (o *openFile) Seek(off int64, whence int) (int64, error) { return o.r.Seek(off, whence) }
func (o *openFile) ReadAt(b []byte, off int64) (int, error)   { return o.r.ReadAt(b, off) }

type openDir struct {
	entry
	entries []fs.DirEntry
	off     int
}

func (d *openDir) Stat() (fs.FileInfo, error) { return d.entry, nil }
func (d *openDir) Close() error               { return nil }
func (d *openDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}

// ReadDir implements fs.ReadDirFile.
func (d *openDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := d.entries[d.off:]
	if n <= 0 {
		d.off = len(d.entries)
		return slices.Clone(rest), nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	n = min(n, len(rest))
	d.off += n
	return slices.Clone(rest[:n]), nil
}
