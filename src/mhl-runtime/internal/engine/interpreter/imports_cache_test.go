package interpreter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
	"github.com/mh-language/mhl-core-runtime/internal/lang/parser"
)

func writeModules(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func resolveCached(t *testing.T, file string, cache *ImportCache) *ast.Program {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	if err := ResolveImportsCached(file, prog, cache); err != nil {
		t.Fatalf("resolve %s: %v", file, err)
	}
	return prog
}

func findAgentDecl(prog *ast.Program, name string) *ast.Agent {
	for _, d := range prog.Decls {
		if d.Agent != nil && d.Agent.Name == name {
			return d.Agent
		}
	}
	return nil
}

// Two entry files importing the same chain resolve it once: the second
// reuses the first's modules, so both see the very same declaration nodes —
// and the same set a cache-less ResolveImports builds.
func TestResolveImportsCachedSharesAcyclicModules(t *testing.T) {
	dir := t.TempDir()
	writeModules(t, dir, map[string]string{
		"lib/base.mh": `export agent Base { command: "echo" }`,
		"lib/mid.mh": `import "base.mh"
export agent Mid { command: "echo" }`,
		"a.mh": `import {Mid} from "lib/mid.mh"
pipeline A { step S { log("a") } }`,
		"b.mh": `import "lib/mid.mh"
pipeline B { step S { log("b") } }`,
	})

	cache := NewImportCache()
	a := resolveCached(t, filepath.Join(dir, "a.mh"), cache)
	if len(cache.modules) != 2 {
		t.Fatalf("expected base.mh and mid.mh cached, got %d modules", len(cache.modules))
	}
	b := resolveCached(t, filepath.Join(dir, "b.mh"), cache)

	for _, name := range []string{"Base", "Mid"} {
		ag, bg := findAgentDecl(a, name), findAgentDecl(b, name)
		if ag == nil || bg == nil {
			t.Fatalf("agent %s missing: a=%v b=%v", name, ag, bg)
		}
		if ag != bg {
			t.Errorf("agent %s: expected one shared declaration node across entry files", name)
		}
	}

	fresh := resolveCached(t, filepath.Join(dir, "b.mh"), nil)
	if len(fresh.Decls) != len(b.Decls) {
		t.Errorf("cached resolution has %d decls, uncached %d", len(b.Decls), len(fresh.Decls))
	}
}

// A resolution that runs into an import cycle shares nothing: what the
// cycle's modules hold depends on where the traversal entered it.
func TestResolveImportsCachedSkipsCyclicResolution(t *testing.T) {
	dir := t.TempDir()
	writeModules(t, dir, map[string]string{
		"x.mh": `import "y.mh"
export agent X { command: "echo" }`,
		"y.mh": `import "x.mh"
export agent Y { command: "echo" }`,
		"main.mh": `import "x.mh"
pipeline P { step S { log("p") } }`,
	})

	cache := NewImportCache()
	resolveCached(t, filepath.Join(dir, "main.mh"), cache)
	if len(cache.modules) != 0 {
		t.Fatalf("expected a cyclic resolution to share no modules, got %d", len(cache.modules))
	}
	// A cycle back to the entry file itself counts too.
	resolveCached(t, filepath.Join(dir, "x.mh"), cache)
	if len(cache.modules) != 0 {
		t.Fatalf("expected a cycle through the entry file to share no modules, got %d", len(cache.modules))
	}
}

// A preloaded module is resolved from the AST handed in, not read from disk —
// the file is gone by the time anything imports it — and only once: the
// first resolution takes it, a later one reuses the shared result.
func TestResolveImportsCachedUsesPreloadedModule(t *testing.T) {
	dir := t.TempDir()
	writeModules(t, dir, map[string]string{
		"lib.mh": `export agent Lib { command: "echo" }`,
		"a.mh":   `import "lib.mh"` + "\n" + `pipeline A { step S { log("a") } }`,
		"b.mh":   `import {Lib} from "lib.mh"` + "\n" + `pipeline B { step S { log("b") } }`,
	})
	lib := filepath.Join(dir, "lib.mh")
	src, err := os.ReadFile(lib)
	if err != nil {
		t.Fatal(err)
	}
	libProg, err := parser.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	cache := NewImportCache()
	cache.Preload(lib, libProg)
	if err := os.Remove(lib); err != nil {
		t.Fatal(err)
	}

	a := resolveCached(t, filepath.Join(dir, "a.mh"), cache)
	b := resolveCached(t, filepath.Join(dir, "b.mh"), cache)
	if got := findAgentDecl(a, "Lib"); got == nil || got != findAgentDecl(libProg, "Lib") {
		t.Errorf("a.mh: expected the preloaded Lib declaration, got %v", got)
	}
	if findAgentDecl(b, "Lib") != findAgentDecl(a, "Lib") {
		t.Error("b.mh: expected the module a.mh resolved, shared")
	}
	if len(cache.parsed) != 0 {
		t.Errorf("expected the preloaded module to be taken, %d left", len(cache.parsed))
	}
}
