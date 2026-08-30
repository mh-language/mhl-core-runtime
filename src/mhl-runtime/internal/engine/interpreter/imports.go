package interpreter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// ResolveImports processes the `use { Names [as Alias] } from "path"`
// declarations at the top level of prog. Each referenced path is resolved
// relative to the directory of file and must exist and parse as a valid .mh
// module; a `use` also requires every named symbol to be declared with
// `export` in that module — including transitively, see below.
//
// Imports are transitive: the module a `use` loads has its own `use`/
// `import` declarations resolved *first* (recursively, relative to *its
// own* directory, not file's, and before this file's requested names are
// even checked against it), and the module's entire resolved declaration
// set — not just the requested names — is merged into prog. This is what
// lets a `tool`/`pipeline` depend on another declaration (e.g. a `memory`
// block) from a third file without every importer of that tool needing to
// know or separately `use` the dependency: a tool method body resolves a
// name like `FeatureStoreMemory.get(...)` by a flat scan over prog.Decls at
// run time (findMemory, memory_ops.go — the same flat-namespace lookup
// findTool/findAgent/findPrompt all use), so it has to actually be *there*,
// however it arrived. It's also what lets a file re-export something it
// only itself `use`d, with no `export` of its own repeating the name: since
// the merged-in declaration is the original AST node from wherever it was
// first declared, its Export bit still reads true, so a third file's own
// `use {Name} from "that-file.mh"` finds it too — resolving that file's own
// imports before checking is what makes the name resolvable there at all.
// `export` still gates what any file may request by name via `use {Name}`
// — nothing bypasses that check, it's just no longer evaluated against an
// only-partially-resolved module. A `test` block never rides along
// (mergeableDecl below excludes it): merging one in would make `mhl test`
// on the importer silently run a suite that belongs to a different file.
func ResolveImports(file string, prog *ast.Program) error {
	prog.AliasMap()
	key := file
	if abs, err := filepath.Abs(file); err == nil {
		key = abs
	}
	// Seeding the cache with the entry file itself closes the same cycle
	// guard around it that every other module gets: if something it
	// (transitively) uses `use`s it back, that lookup finds prog — instead
	// of loadModule-ing and recursing into file all over again, forever.
	return resolveImports(file, prog, map[string]*ast.Program{key: prog})
}

// resolveImports is ResolveImports' recursive core. resolved caches every
// module reached so far, keyed by absolute path ("./a.mh" and "../x/a.mh"
// from different directories still dedupe to the same key) — a module is
// loaded and recursively resolved *once* per top-level ResolveImports call,
// no matter how many different files `use` something from it (a diamond
// dependency), so every one of them sees the same, fully-merged
// declaration set rather than some seeing a partial one depending on
// resolution order. The cache entry is recorded *before* recursing into
// that module's own imports, which is what stops a cyclic chain (A uses B
// uses A) from recursing forever — the same shape
// maxStepVisits/maxLoopIterations guard elsewhere in this codebase, just
// for the import graph instead of control flow; a name that only becomes
// resolvable via the cycle's own not-yet-finished side simply won't be
// found, the same inherent limit any circular-dependency graph has.
func resolveImports(file string, prog *ast.Program, resolved map[string]*ast.Program) error {
	prog.AliasMap()
	dir := filepath.Dir(file)
	for _, decl := range prog.Decls {
		switch {
		case decl.Prompt != nil && decl.Prompt.Source != "":
			text, err := loadPromptSource(dir, decl.Prompt.Source)
			if err != nil {
				return fmt.Errorf("prompt %q from %q: %w", decl.Prompt.Name, decl.Prompt.Source, err)
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
					return fmt.Errorf("use {%s} from %q: %w", strings.Join(decl.Use.Names(), ", "), decl.Use.Path, err)
				}
				resolved[key] = module
				if err := resolveImports(modulePath, module, resolved); err != nil {
					return fmt.Errorf("use {%s} from %q: %w", strings.Join(decl.Use.Names(), ", "), decl.Use.Path, err)
				}
			}

			if err := mergeAliases(prog, module.AliasMap()); err != nil {
				return fmt.Errorf("use {%s} from %q: %w", strings.Join(decl.Use.Names(), ", "), decl.Use.Path, err)
			}
			for _, item := range decl.Use.Items {
				if _, ok := findExport(module, item.Name); !ok {
					return fmt.Errorf("use {%s} from %q: %q is not exported", strings.Join(decl.Use.Names(), ", "), decl.Use.Path, item.Name)
				}
				if item.Alias != "" {
					if err := addAlias(prog, item.Alias, item.Name); err != nil {
						return fmt.Errorf("use {%s} from %q: %w", strings.Join(decl.Use.Names(), ", "), decl.Use.Path, err)
					}
				}
			}

			for _, imported := range module.Decls {
				kind, name, ok := mergeableDecl(imported)
				if !ok || declPresent(prog.Decls, kind, name) {
					continue
				}
				prog.Decls = append(prog.Decls, imported)
			}
		}
	}
	return nil
}

// addAlias records one source-level alias and rejects an ambiguous binding.
// Repeating the same alias for the same declaration is harmless, which keeps
// diamond-shaped import graphs deterministic.
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

// resolveName maps a local alias to the declaration name used by the
// flattened AST. Names without an alias are returned unchanged.
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
// stable enough to dedupe on. Use/Import wrappers carry nothing worth
// keeping once resolved, and Test blocks belong only to the file that
// declared them — see ResolveImports' doc comment for why both are
// excluded.
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
// with this exact (kind, name) — a diamond dependency (two different
// `use`s eventually pulling in the same module) would otherwise append the
// same declaration twice; harmless for the flat-scan lookups that read it,
// but pointless bloat.
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
// `prompt ... from "path"` declaration. The trailing TrimSpace mirrors
// trimMultiline's treatment of an inline """...""" body (internal/lang/parser/parser.go)
// so a file-sourced and an inline-sourced prompt body are indistinguishable
// from here on.
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
