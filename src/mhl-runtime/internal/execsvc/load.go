package execsvc

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/engine/interpreter"
	"github.com/mh-language/mhl-core-runtime/internal/engine/runtime"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// Workflow is one parsed, import-resolved pipeline/workflow declaration, kept
// ready to describe (Pipeline.Inputs / InputSchema) and to run (Program +
// File feed a Request). A server adapter loads a directory of these once at
// startup and reuses them across requests.
type Workflow struct {
	Name     string
	File     string
	Program  *ast.Program
	Pipeline runtime.Pipeline
	// IsWorkflow reports whether the `workflow` keyword was used (vs
	// `pipeline`) — for a human-readable description only.
	IsWorkflow bool
	// Loop reports the `loop` prefix.
	Loop bool
}

// File is one .mh file under a workflow directory, parsed but not yet
// import-resolved — what ParseDir returns and LoadFiles consumes.
type File struct {
	Path    string
	Program *ast.Program
}

// ParseDir reads and parses every .mh file under dir (recursively, sorted by
// path). A caller that has to inspect a directory's declarations before
// loading it (`mhl serve mcp --http` looks for an `extension store`) parses
// it once with this and hands the result to LoadFiles.
func ParseDir(dir string) ([]File, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".mh") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	files := make([]File, 0, len(paths))
	for _, f := range paths {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}
		prog, err := parser.Parse(string(src))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f, err)
		}
		files = append(files, File{Path: f, Program: prog})
	}
	return files, nil
}

// Load parses every .mh file under dir and returns one Workflow per declared
// pipeline/workflow and workflow alias, keyed by declaration name. A name declared in two files
// is an error, and a directory that declares none is an error.
func Load(dir string) (map[string]Workflow, error) {
	files, err := ParseDir(dir)
	if err != nil {
		return nil, err
	}
	return LoadFiles(dir, files)
}

// LoadFiles is Load over files already parsed by ParseDir(dir); dir only
// labels errors. It resolves each Program's imports in place, so files must
// not be reused afterwards.
func LoadFiles(dir string, files []File) (map[string]Workflow, error) {
	out := map[string]Workflow{}
	// One cache for the whole directory: a module imported (directly or
	// transitively) by many files is parsed and resolved once, not once per
	// importer.
	imports := interpreter.NewImportCache()
	for _, file := range files {
		f, prog := file.Path, file.Program
		// Only what this file itself declares is registered here: an
		// imported pipeline/workflow is merged into prog too (it has to be,
		// to run), but it belongs to — and is registered by — the file that
		// declares it. Without this, a shared workflow imported by two files
		// (the target of two workflow aliases, typically) was reported as
		// "declared in more than one file".
		own := map[string]bool{}
		for _, d := range prog.Decls {
			if d.Pipeline != nil {
				own[d.Pipeline.Name] = true
			}
			if d.Alias != nil {
				own[d.Alias.Name] = true
			}
		}
		// A file that declares no pipeline/workflow or alias registers
		// nothing (only what a file itself declares is registered, see
		// above), so its imports needn't be resolved here at all — a
		// library module is resolved only as part of the entry files that
		// import it.
		if len(own) == 0 {
			continue
		}
		if err := interpreter.ResolveImportsCached(f, prog, imports); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, d := range prog.Decls {
			// An `internal` declaration is not an entry point: not published.
			if (d.Alias != nil && d.Alias.Internal) || (d.Pipeline != nil && d.Pipeline.Internal) {
				continue
			}
			if a := d.Alias; a != nil && own[a.Name] {
				if _, dup := out[a.Name]; dup {
					return nil, fmt.Errorf("%q declared in more than one file under %s", a.Name, dir)
				}
				p, err := runtime.FindPipeline(prog, a.Name)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", f, err)
				}
				out[a.Name] = Workflow{Name: a.Name, File: f, Program: prog, Pipeline: p, IsWorkflow: a.Kind == "workflow", Loop: p.Loop}
				continue
			}
			if d.Pipeline == nil || !own[d.Pipeline.Name] {
				continue
			}
			// A directory of workflows is scanned file by file: a `partial`
			// fragment resolved on its own (nothing here pulled its
			// siblings in via a whole-file `import`) is expected to be
			// incomplete — that's not this fragment's file being broken,
			// it's just not the file that assembles the whole declaration,
			// so it's skipped rather than registered (or, worse, failing
			// every other workflow in dir because this one file's Pipeline
			// decl shares a Name with the file that actually completes it).
			if d.Pipeline.Partial && d.Pipeline.EntryStepCount() != 1 {
				continue
			}
			name := d.Pipeline.Name
			if _, dup := out[name]; dup {
				return nil, fmt.Errorf("%q declared in more than one file under %s", name, dir)
			}
			p, err := runtime.FindPipeline(prog, name)
			if err != nil {
				return nil, err
			}
			out[name] = Workflow{
				Name:       name,
				File:       f,
				Program:    prog,
				Pipeline:   p,
				IsWorkflow: d.Pipeline.IsWorkflow(),
				Loop:       d.Pipeline.Loop,
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no pipeline or workflow declared under %s", dir)
	}
	return out, nil
}

// KindLabel is a human phrase like "workflow", "loop pipeline".
func (w Workflow) KindLabel() string {
	k := "pipeline"
	if w.IsWorkflow {
		k = "workflow"
	}
	if w.Loop {
		k = "loop " + k
	}
	return k
}
