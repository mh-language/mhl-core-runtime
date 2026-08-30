package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// mergeImports statically validates every `use { Names [as Alias] } from
// "path"` declaration at the top level of prog (whose source file is file),
// resolving each path relative to file's directory. It never aborts early:
// every broken use is reported as a Finding, and
// within a single `use {A, B, C}` every unresolved name is reported, not
// just the first. It returns a copy of prog with imports merged in,
// mirroring internal/engine/interpreter.ResolveImports (including its
// transitivity — see resolveImportsInto below) so later checks
// (checkAgentCalls) see everything the runtime would actually have
// resolved by the time a step runs, not just the top-level requested
// names.
func mergeImports(file string, prog *ast.Program) (*ast.Program, []Finding) {
	merged := &ast.Program{Decls: append([]*ast.Declaration{}, prog.Decls...)}
	merged.AliasMap()
	var findings []Finding
	key := file
	if abs, err := filepath.Abs(file); err == nil {
		key = abs
	}
	// Seeding the cache with the entry file itself is what stops something
	// it (transitively) uses from `use`-ing it right back and recursing
	// forever — see resolveImportsInto's doc comment.
	resolveImportsInto(file, prog, merged, map[string]*ast.Program{key: prog}, &findings)
	return merged, findings
}

// resolveImportsInto walks prog's `use`/`import` declarations (whose source
// file is file) and appends whatever they resolve to onto merged.Decls —
// the caller's growing, flattened set, not prog's own. Every `use` is
// resolved transitively: the module it loads has its own imports resolved
// *first* (recursively, relative to *its own* directory, and before this
// file's requested names are even checked against it), and that module's
// entire resolved declaration set — not just the requested names — is what
// gets appended, mirroring internal/engine/interpreter.resolveImports
// exactly, so lint sees the same name resolution the runtime does
// (including a file re-exporting something it only itself `use`d, with no
// `export` of its own repeating the name — checking after resolving is
// what makes that resolvable here too, not just at run time).
//
// resolved caches every module reached so far, keyed by absolute path — a
// module is loaded and recursively resolved *once* per mergeImports call,
// no matter how many different files `use` something from it (a diamond
// dependency), so every one of them sees the same, fully-merged
// declaration set rather than some seeing a partial one depending on
// resolution order. The cache entry is recorded before recursing into that
// module's own imports, which is what stops a cyclic chain (A uses B uses
// A) from recursing forever — the same shape maxStepVisits guards a
// runaway `goto` elsewhere in this codebase, just for the import graph
// instead of control flow.
func resolveImportsInto(file string, prog *ast.Program, merged *ast.Program, resolved map[string]*ast.Program, findings *[]Finding) {
	prog.AliasMap()
	dir := filepath.Dir(file)
	for _, decl := range prog.Decls {
		switch {
		case decl.Prompt != nil && decl.Prompt.Source != "":
			text, err := loadPromptSource(dir, decl.Prompt.Source)
			if err != nil {
				*findings = append(*findings, Finding{
					File: file, Line: decl.Prompt.Pos.Line, Column: decl.Prompt.Pos.Column,
					Message: fmt.Sprintf("prompt %q from %q: %s", decl.Prompt.Name, decl.Prompt.Source, err),
				})
				continue
			}
			decl.Prompt.Body = ast.NewMultilineStringExpr(text)
		case decl.Use != nil:
			modulePath := filepath.Join(dir, decl.Use.Path)
			key := modulePath
			if abs, err := filepath.Abs(modulePath); err == nil {
				key = abs
			}

			module, ok := resolved[key]
			if !ok {
				var err error
				module, err = loadModule(dir, decl.Use.Path)
				if err != nil {
					*findings = append(*findings, Finding{
						File: file, Line: decl.Use.Pos.Line, Column: decl.Use.Pos.Column,
						Message: fmt.Sprintf("use {%s} from %q: %s", strings.Join(decl.Use.Names(), ", "), decl.Use.Path, err),
					})
					continue
				}
				resolved[key] = module
				resolveImportsInto(modulePath, module, module, resolved, findings)
			}

			missing := false
			if err := mergeAliases(merged, module.AliasMap()); err != nil {
				*findings = append(*findings, Finding{
					File: file, Line: decl.Use.Pos.Line, Column: decl.Use.Pos.Column,
					Message: fmt.Sprintf("use {%s} from %q: %s", strings.Join(decl.Use.Names(), ", "), decl.Use.Path, err),
				})
				missing = true
			}
			for _, item := range decl.Use.Items {
				if _, found := findExport(module, item.Name); !found {
					*findings = append(*findings, Finding{
						File: file, Line: decl.Use.Pos.Line, Column: decl.Use.Pos.Column,
						Message: fmt.Sprintf("use {%s} from %q: %q is not exported", strings.Join(decl.Use.Names(), ", "), decl.Use.Path, item.Name),
					})
					missing = true
					continue
				}
				if item.Alias != "" {
					if err := addAlias(merged, item.Alias, item.Name); err != nil {
						*findings = append(*findings, Finding{
							File: file, Line: decl.Use.Pos.Line, Column: decl.Use.Pos.Column,
							Message: fmt.Sprintf("use {%s} from %q: %s", strings.Join(decl.Use.Names(), ", "), decl.Use.Path, err),
						})
						missing = true
					}
				}
			}
			if missing {
				continue
			}

			for _, imported := range module.Decls {
				kind, name, mergeable := mergeableDecl(imported)
				if !mergeable || declPresent(merged.Decls, kind, name) {
					continue
				}
				merged.Decls = append(merged.Decls, imported)
			}
		}
	}
}

func addAlias(prog *ast.Program, alias, name string) error {
	aliases := prog.AliasMap()
	if existing, ok := aliases[alias]; ok {
		if existing == name {
			return nil
		}
		return fmt.Errorf("alias %q already refers to %q", alias, existing)
	}
	aliases[alias] = name
	return nil
}

func mergeAliases(dst *ast.Program, aliases map[string]string) error {
	for alias, name := range aliases {
		if err := addAlias(dst, alias, name); err != nil {
			return err
		}
	}
	return nil
}

func resolveName(prog *ast.Program, name string) string {
	if prog == nil {
		return name
	}
	aliases := prog.AliasMap()
	seen := map[string]bool{}
	for {
		if seen[name] {
			return name
		}
		seen[name] = true
		resolved, ok := aliases[name]
		if !ok {
			return name
		}
		name = resolved
	}
}

// mergeableDecl reports whether decl is a kind that belongs in another
// program's Decls once its module is used at all, plus a (kind, name) pair
// stable enough to dedupe on — mirrors
// internal/engine/interpreter.mergeableDecl exactly. Use/Import wrappers
// carry nothing worth keeping once resolved, and a `test` block belongs
// only to the file that declared it, never to whatever imports something
// else from that file.
func mergeableDecl(decl *ast.Declaration) (kind, name string, ok bool) {
	switch {
	case decl.Prompt != nil:
		return "prompt", decl.Prompt.Name, true
	case decl.Extension != nil:
		return "extension:" + decl.Extension.Kind, decl.Extension.Name, true
	case decl.Agent != nil:
		return "agent", decl.Agent.Name, true
	case decl.Memory != nil:
		return "memory", decl.Memory.Name, true
	case decl.Tool != nil:
		return "tool", decl.Tool.Name, true
	case decl.Pipeline != nil:
		return "pipeline", decl.Pipeline.Name, true
	case decl.Type != nil:
		return "type", decl.Type.Name, true
	case decl.Enum != nil:
		return "enum", decl.Enum.Name, true
	default:
		return "", "", false
	}
}

// declPresent reports whether decls already has a mergeable declaration
// with this exact (kind, name) — see mergeableDecl.
func declPresent(decls []*ast.Declaration, kind, name string) bool {
	for _, d := range decls {
		if k, n, ok := mergeableDecl(d); ok && k == kind && n == name {
			return true
		}
	}
	return false
}

// loadModule reads and parses the .mh file at path, resolved relative to
// dir.
func loadModule(dir, path string) (*ast.Program, error) {
	full := filepath.Join(dir, path)
	src, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	module, err := parser.Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", full, err)
	}
	return module, nil
}

// loadPromptSource reads the file at path, resolved relative to dir, for a
// `prompt ... from "path"` declaration — mirrors
// internal/engine/interpreter.loadPromptSource exactly, including the
// trailing TrimSpace that matches trimMultiline's treatment of an inline
// """...""" body (internal/lang/parser/parser.go).
func loadPromptSource(dir, path string) (string, error) {
	full := filepath.Join(dir, path)
	src, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(src)), nil
}

// findExport returns the declaration in module exporting name, if any.
func findExport(module *ast.Program, name string) (*ast.Declaration, bool) {
	for _, decl := range module.Decls {
		if !decl.Export {
			continue
		}
		switch {
		case decl.Agent != nil && decl.Agent.Name == name:
			return decl, true
		case decl.Extension != nil && decl.Extension.Name == name:
			return decl, true
		case decl.Memory != nil && decl.Memory.Name == name:
			return decl, true
		case decl.Tool != nil && decl.Tool.Name == name:
			return decl, true
		case decl.Pipeline != nil && decl.Pipeline.Name == name:
			return decl, true
		case decl.Prompt != nil && decl.Prompt.Name == name:
			return decl, true
		case decl.Type != nil && decl.Type.Name == name:
			return decl, true
		case decl.Enum != nil && decl.Enum.Name == name:
			return decl, true
		}
	}
	return nil, false
}
