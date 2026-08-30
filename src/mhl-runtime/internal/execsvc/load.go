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

// Load parses every .mh file under dir and returns one Workflow per declared
// pipeline/workflow, keyed by declaration name. A name declared in two files
// is an error, and a directory that declares none is an error.
func Load(dir string) (map[string]Workflow, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".mh") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	out := map[string]Workflow{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}
		prog, err := parser.Parse(string(src))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f, err)
		}
		if err := interpreter.ResolveImports(f, prog); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, d := range prog.Decls {
			if d.Pipeline == nil {
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
