// Package lint statically analyzes .mh files for problems that today only
// surface at `mhl run` time (broken imports, calls to undeclared agents,
// misconfigured agents, syntax errors), and reports them with a file and
// line number instead of requiring an actual pipeline execution to hit them.
package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
	"github.com/mh-language/mhl-core-runtime/internal/lang/types"
)

// Finding is one statically-detected problem in a .mh file.
type Finding struct {
	File    string
	Line    int
	Column  int
	Message string
}

// String formats the finding as "<file>:<line>: error: <message>".
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: error: %s", f.File, f.Line, f.Message)
}

// File reads, parses and statically checks the .mh file at path. A read or
// parse failure is reported as a Finding rather than a Go error, so a single
// broken file never aborts a larger Dir scan.
func File(path string) []Finding {
	src, err := os.ReadFile(path)
	if err != nil {
		return []Finding{{File: path, Message: err.Error()}}
	}
	return Source(path, string(src))
}

// Source parses and statically checks src as if it were the .mh file at
// path, without reading path off disk itself — path is still used to
// resolve relative import/use targets (mergeImports) and to label findings.
// This is what File delegates to, and it is also the seam an editor
// integration (see internal/lsp) needs: the buffer being edited may not
// match, or may not even exist, on disk yet.
func Source(path, src string) []Finding {
	prog, err := parser.Parse(src)
	if err != nil {
		return []Finding{findingFromParseError(path, err)}
	}

	merged, findings := mergeImports(path, prog)
	aliases, aliasErrs := types.Aliases(merged)
	for _, e := range aliasErrs {
		findings = append(findings, Finding{File: path, Line: e.Line, Column: e.Column, Message: e.Message})
	}
	findings = append(findings, checkAgentCalls(path, merged, aliases)...)
	findings = append(findings, checkParallelGroups(path, merged)...)
	findings = append(findings, checkPipelineGoto(path, merged)...)
	findings = append(findings, checkPipelineProperties(path, merged)...)
	findings = append(findings, checkAgentProperties(path, merged)...)
	findings = append(findings, checkToolBlocks(path, merged, aliases)...)
	findings = append(findings, checkLoopStopWhen(path, merged)...)
	findings = append(findings, checkPipelineStepTimeout(path, merged)...)
	findings = append(findings, checkPipelineContext(path, merged)...)
	findings = append(findings, checkPipelineInputTypes(path, merged, aliases)...)
	findings = append(findings, checkToolMethodReturnTypes(path, merged, aliases)...)
	findings = append(findings, checkParamDefaults(path, merged)...)
	findings = append(findings, checkConstReassign(path, merged)...)
	findings = append(findings, checkExtensions(path, merged)...)
	return findings
}

// Dir recursively scans dir for .mh files (sorted) and returns the
// concatenation of File(f) for every file found, in filename order.
func Dir(dir string) ([]Finding, error) {
	files, err := findMHLFiles(dir)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, f := range files {
		findings = append(findings, File(f)...)
	}
	return findings, nil
}

// findMHLFiles returns the sorted list of .mh files under dir (recursively).
func findMHLFiles(dir string) ([]string, error) {
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
	return files, nil
}
