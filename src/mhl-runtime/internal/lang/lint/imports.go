package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/frontmatter"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

// mergeImports statically validates every `import { Names [as Alias] } from
// "path"` declaration at the top level of prog (whose source file is file),
// resolving each path relative to file's directory. It never aborts early:
// every broken import is reported as a Finding, and
// within a single `import {A, B, C}` every unresolved name is reported, not
// just the first. It returns a copy of prog with imports merged in,
// mirroring internal/engine/interpreter.ResolveImports (including its
// transitivity — see resolveImportsInto below) so later checks
// (checkAgentCalls) see everything the runtime would actually have
// resolved by the time a step runs, not just the top-level requested
// names.
//
// cache, when non-nil, shares resolved modules with earlier mergeImports
// calls (Dir) — see importCache.
func mergeImports(file string, prog *ast.Program, cache *importCache) (*ast.Program, []Finding) {
	merged := &ast.Program{Decls: append([]*ast.Declaration{}, prog.Decls...)}
	merged.AliasMap()
	var findings []Finding
	key := file
	if abs, err := filepath.Abs(file); err == nil {
		key = abs
	}
	// Seeding the set with the entry file itself is what stops something
	// it (transitively) uses from `import`-ing it right back and recursing
	// forever — see resolveImportsInto's doc comment.
	set := &moduleSet{
		local:      map[string]*ast.Program{key: prog},
		inProgress: map[string]bool{key: true},
		recs:       map[string]*cachedModule{},
	}
	if cache != nil {
		set.shared = cache.modules
	}
	resolveImportsInto(file, prog, merged, set, frameOut{findings: &findings})
	if cache != nil && !set.cyclic {
		for k, c := range set.recs {
			cache.modules[k] = c
		}
	}
	return merged, findings
}

// importCache shares resolved modules across the mergeImports calls of one
// Dir scan, so a module imported by many files is read and parsed once.
// Unlike interpreter.ImportCache it must also keep lint's output unchanged:
// a module's findings are reported once per entry file that reaches it, in
// depth-first order. So each cached module carries the record of what its
// resolution emitted — its own findings and, in order, the modules it
// imports — and a cache hit replays that record (see moduleSet.replay)
// instead of re-resolving. Only acyclic resolutions are shared, as in
// interpreter.ImportCache.
type importCache struct {
	modules map[string]*cachedModule
}

func newImportCache() *importCache {
	return &importCache{modules: map[string]*cachedModule{}}
}

type cachedModule struct {
	prog   *ast.Program
	events []importEvent
}

// importEvent is one step of a module's recorded resolution: a finding it
// emitted itself, or (dep non-empty) the point where it imported module dep,
// whose own findings follow there unless already reported in this call.
type importEvent struct {
	finding Finding
	dep     string
}

// moduleSet is one mergeImports call's view of the modules: local holds
// every module already reached by this call (so its findings are not
// reported twice), shared those resolved by earlier calls.
type moduleSet struct {
	local      map[string]*ast.Program
	shared     map[string]*cachedModule
	inProgress map[string]bool
	cyclic     bool
	// recs records the resolution of every module this call loaded itself.
	recs map[string]*cachedModule
}

// lookup returns the module at key, reporting the findings of a shared
// module (and of whatever it imports) the first time this call reaches it.
func (s *moduleSet) lookup(key string, findings *[]Finding) (*ast.Program, bool) {
	if m, ok := s.local[key]; ok {
		if s.inProgress[key] {
			s.cyclic = true
		}
		return m, true
	}
	c, ok := s.shared[key]
	if !ok {
		return nil, false
	}
	s.replay(key, c, findings)
	return c.prog, true
}

func (s *moduleSet) replay(key string, c *cachedModule, findings *[]Finding) {
	s.local[key] = c.prog
	for _, ev := range c.events {
		if ev.dep == "" {
			*findings = append(*findings, ev.finding)
			continue
		}
		if _, seen := s.local[ev.dep]; seen {
			continue
		}
		if d, ok := s.shared[ev.dep]; ok {
			s.replay(ev.dep, d, findings)
		}
	}
}

// frameOut is where one resolveImportsInto frame reports: the call's
// findings, plus, for a module being recorded for the cache, its events.
type frameOut struct {
	findings *[]Finding
	events   *[]importEvent
}

func (o frameOut) emit(f Finding) {
	*o.findings = append(*o.findings, f)
	if o.events != nil {
		*o.events = append(*o.events, importEvent{finding: f})
	}
}

func (o frameOut) dep(key string) {
	if o.events != nil {
		*o.events = append(*o.events, importEvent{dep: key})
	}
}

// resolveImportsInto walks prog's `import` declarations (whose source
// file is file) and appends whatever they resolve to onto merged.Decls —
// the caller's growing, flattened set, not prog's own. Every `import` is
// resolved transitively: the module it loads has its own imports resolved
// *first* (recursively, relative to *its own* directory, and before this
// file's requested names are even checked against it), and that module's
// entire resolved declaration set — not just the requested names — is what
// gets appended, mirroring internal/engine/interpreter.resolveImports
// exactly, so lint sees the same name resolution the runtime does
// (including a file re-exporting something it only itself `import`ed, with no
// `export` of its own repeating the name — checking after resolving is
// what makes that resolvable here too, not just at run time).
//
// resolved holds every module reached so far, keyed by absolute path — a
// module is loaded and recursively resolved *once* per mergeImports call,
// no matter how many different files `import` something from it (a diamond
// dependency), so every one of them sees the same, fully-merged
// declaration set rather than some seeing a partial one depending on
// resolution order. The cache entry is recorded before recursing into that
// module's own imports, which is what stops a cyclic chain (A uses B uses
// A) from recursing forever — the same shape maxStepVisits guards a
// runaway `goto` elsewhere in this codebase, just for the import graph
// instead of control flow.
func resolveImportsInto(file string, prog *ast.Program, merged *ast.Program, resolved *moduleSet, out frameOut) {
	prog.AliasMap()
	dir := filepath.Dir(file)
	// See interpreter.resolveImports: indexed once, on the first import.
	var present *declIndex
	for _, decl := range prog.Decls {
		switch {
		case decl.Prompt != nil && decl.Prompt.Source != "":
			fm, text, err := loadPromptSource(dir, decl.Prompt.Source)
			if err != nil {
				out.emit(Finding{
					File: file, Line: decl.Prompt.Pos.Line, Column: decl.Prompt.Pos.Column,
					Message: fmt.Sprintf("prompt %q from %q: %s", decl.Prompt.Name, decl.Prompt.Source, err),
				})
				continue
			}
			decl.Prompt.Frontmatter = fm
			decl.Prompt.Body = ast.NewMultilineStringExpr(text)
		case decl.Skill != nil && decl.Skill.Source != "":
			fm, content, err := loadSkillSource(dir, decl.Skill.Source)
			if err != nil {
				out.emit(Finding{
					File: file, Line: decl.Skill.Pos.Line, Column: decl.Skill.Pos.Column,
					Message: fmt.Sprintf("skill %q from %q: %s", decl.Skill.Name, decl.Skill.Source, err),
				})
				continue
			}
			decl.Skill.Frontmatter = fm
			decl.Skill.Content = content
		case decl.Schema != nil:
			if err := decl.Schema.Load(dir); err != nil {
				out.emit(Finding{
					File: file, Line: decl.Schema.Pos.Line, Column: decl.Schema.Pos.Column,
					Message: fmt.Sprintf("schema %q from %q: %s", decl.Schema.Name, decl.Schema.Source, err),
				})
			}
		case decl.Import != nil:
			label := importLabel(decl.Import)
			modulePath := filepath.Join(dir, decl.Import.Path)
			key := modulePath
			if abs, err := filepath.Abs(modulePath); err == nil {
				key = abs
			}

			module, ok := resolved.lookup(key, out.findings)
			if !ok {
				var err error
				module, err = loadModule(dir, decl.Import.Path)
				if err != nil {
					out.emit(Finding{
						File: file, Line: decl.Import.Pos.Line, Column: decl.Import.Pos.Column,
						Message: fmt.Sprintf("%s: %s", label, err),
					})
					continue
				}
				resolved.local[key] = module
				resolved.inProgress[key] = true
				rec := &cachedModule{prog: module}
				resolveImportsInto(modulePath, module, module, resolved, frameOut{findings: out.findings, events: &rec.events})
				delete(resolved.inProgress, key)
				resolved.recs[key] = rec
			}
			out.dep(key)

			missing := false
			if err := mergeAliases(merged, module.AliasMap()); err != nil {
				out.emit(Finding{
					File: file, Line: decl.Import.Pos.Line, Column: decl.Import.Pos.Column,
					Message: fmt.Sprintf("%s: %s", label, err),
				})
				missing = true
			}
			for _, item := range decl.Import.Items {
				if _, found := findExport(module, item.Name); !found {
					out.emit(Finding{
						File: file, Line: decl.Import.Pos.Line, Column: decl.Import.Pos.Column,
						Message: fmt.Sprintf("%s: %q is not exported", label, item.Name),
					})
					missing = true
					continue
				}
				if item.Alias != "" {
					if err := addAlias(merged, item.Alias, item.Name); err != nil {
						out.emit(Finding{
							File: file, Line: decl.Import.Pos.Line, Column: decl.Import.Pos.Column,
							Message: fmt.Sprintf("%s: %s", label, err),
						})
						missing = true
					}
				}
			}
			if missing {
				continue
			}

			if present == nil {
				present = newDeclIndex(merged.Decls)
			}
			for _, imported := range module.Decls {
				kind, name, mergeable := mergeableDecl(imported)
				if !mergeable {
					continue
				}
				// See interpreter.resolveImports' matching branch: a
				// `partial` fragment is deduped by identity, not by
				// (kind, name), since sharing a name with another fragment
				// is the whole point.
				if imported.Pipeline != nil && imported.Pipeline.Partial {
					if present.hasFragment(imported.Pipeline) {
						continue
					}
					merged.Decls = append(merged.Decls, imported)
					present.add(imported)
					continue
				}
				if present.has(kind, name) {
					continue
				}
				if imported.Tool != nil {
					imported.Tool.Imported = true
				}
				merged.Decls = append(merged.Decls, imported)
				present.add(imported)
			}
		}
	}
}

// importLabel formats an `import` declaration for a Finding message: `import
// {A, B} from "path"` for the named form, `import "path"` for the bare
// whole-file form (Import.IsWhole) — mirrors interpreter.importLabel.
func importLabel(imp *ast.Import) string {
	if imp.IsWhole() {
		return fmt.Sprintf("import %q", imp.Path)
	}
	return fmt.Sprintf("import {%s} from %q", strings.Join(imp.Names(), ", "), imp.Path)
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
// internal/engine/interpreter.mergeableDecl exactly. Import wrappers
// carry nothing worth keeping once resolved, and a `test` block belongs
// only to the file that declared it, never to whatever imports something
// else from that file.
func mergeableDecl(decl *ast.Declaration) (kind, name string, ok bool) {
	switch {
	case decl.Prompt != nil:
		return "prompt", decl.Prompt.Name, true
	case decl.Skill != nil:
		return "skill", decl.Skill.Name, true
	case decl.Schema != nil:
		return "schema", decl.Schema.Name, true
	case decl.Alias != nil:
		return "pipeline", decl.Alias.Name, true
	case decl.Extension != nil:
		return "extension:" + decl.Extension.Kind, decl.Extension.Name, true
	case decl.Agent != nil:
		return "agent", decl.Agent.Name, true
	case decl.Router != nil:
		return "router", decl.Router.Name, true
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

// declIndex indexes a program's Decls for the import merge's dedup checks —
// mirrors interpreter.declIndex.
type declIndex struct {
	names map[string]bool
	frags map[*ast.Pipeline]bool
}

func newDeclIndex(decls []*ast.Declaration) *declIndex {
	idx := &declIndex{names: map[string]bool{}, frags: map[*ast.Pipeline]bool{}}
	for _, d := range decls {
		idx.add(d)
	}
	return idx
}

func (idx *declIndex) add(d *ast.Declaration) {
	if kind, name, ok := mergeableDecl(d); ok {
		idx.names[kind+"\x00"+name] = true
	}
	if d.Pipeline != nil {
		idx.frags[d.Pipeline] = true
	}
}

// has reports whether the program already has a mergeable declaration with
// this exact (kind, name) — see mergeableDecl.
func (idx *declIndex) has(kind, name string) bool {
	return idx.names[kind+"\x00"+name]
}

// hasFragment is has' counterpart for a `partial` pipeline/workflow
// fragment: identity, not (kind, name).
func (idx *declIndex) hasFragment(frag *ast.Pipeline) bool {
	return idx.frags[frag]
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
// optional leading frontmatter block and the trailing TrimSpace that
// matches trimMultiline's treatment of an inline """...""" body
// (internal/lang/parser/parser.go).
func loadPromptSource(dir, path string) (map[string]any, string, error) {
	full := filepath.Join(dir, path)
	src, err := os.ReadFile(full)
	if err != nil {
		return nil, "", err
	}
	return frontmatter.Parse(string(src))
}

// maxSkillSourceSize mirrors internal/engine/interpreter's constant of the
// same name — see its doc comment.
const maxSkillSourceSize = 64 * 1024

// loadSkillSource reads and parses the SKILL.md file at path, resolved
// relative to dir, for a `skill ... from "path"` declaration — mirrors
// internal/engine/interpreter.loadSkillSource exactly.
func loadSkillSource(dir, path string) (map[string]any, string, error) {
	full := filepath.Join(dir, path)
	src, err := os.ReadFile(full)
	if err != nil {
		return nil, "", err
	}
	if len(src) > maxSkillSourceSize {
		return nil, "", fmt.Errorf("%d bytes exceeds the %d byte limit", len(src), maxSkillSourceSize)
	}
	fm, content, err := frontmatter.Parse(string(src))
	if err != nil {
		return nil, "", err
	}
	if err := frontmatter.RequireKeys(fm, "name", "description"); err != nil {
		return nil, "", err
	}
	return fm, content, nil
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
		case decl.Router != nil && decl.Router.Name == name:
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
		case decl.Schema != nil && decl.Schema.Name == name:
			return decl, true
		case decl.Alias != nil && decl.Alias.Name == name:
			return decl, true
		case decl.Skill != nil && decl.Skill.Name == name:
			return decl, true
		case decl.Type != nil && decl.Type.Name == name:
			return decl, true
		case decl.Enum != nil && decl.Enum.Name == name:
			return decl, true
		}
	}
	return nil, false
}
