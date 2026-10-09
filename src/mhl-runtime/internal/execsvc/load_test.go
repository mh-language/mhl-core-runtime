package execsvc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
)

// TestLoadDirectoryWithPartialFragmentDoesNotFailOtherWorkflows is the
// regression this exists for: Load scans every .mh file under dir
// independently (it's what `mhl serve mcp <dir>` uses to register one MCP
// tool per workflow), so a `partial` fragment sitting in that directory —
// resolved alone, its siblings never pulled in by anything in this
// particular file — legitimately has zero `entry` steps. That must not
// crash Load for the whole directory (it did, before Load learned to skip
// an incomplete partial fragment instead of registering-or-erroring on
// every Pipeline decl it finds): the primary file's own resolveImports
// pulls the fragment in via a whole-file `import`, merges it, and *that*
// declaration — complete, with its one entry step — is what should end up
// registered under "Discovery". A second, unrelated, ordinary pipeline in
// the same directory proves the fragment doesn't take the rest down with
// it.
func TestLoadDirectoryWithPartialFragmentDoesNotFailOtherWorkflows(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"fragment.mh": `
partial workflow Discovery {
    step Gate {
        log("gate reached")
    }
}
`,
		"main.mh": `
import "fragment.mh"

partial workflow Discovery {
    entry step Dispatch {
        log("dispatch reached")
        goto Gate
    }
}
`,
		"other.mh": `
pipeline Other {
    step S {
        log("other")
    }
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	workflows, err := execsvc.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(workflows) != 2 {
		t.Fatalf("expected 2 registered workflows (Discovery, Other), got %d: %+v", len(workflows), workflows)
	}
	discovery, ok := workflows["Discovery"]
	if !ok {
		t.Fatalf("expected \"Discovery\" to be registered, got %+v", workflows)
	}
	if got, want := discovery.Pipeline.Steps, []string{"Dispatch", "Gate"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Discovery.Pipeline.Steps = %v, want %v", got, want)
	}
	if _, ok := workflows["Other"]; !ok {
		t.Fatalf("expected \"Other\" to be registered too, got %+v", workflows)
	}
}

// Two workflow aliases of one shared workflow, each in its own file: every
// declaration is registered exactly once (by the file that declares it), each
// alias publishes only its free inputs, and each runs the shared steps with its
// own bound value under its own name.
func TestLoadAndRunWorkflowAliases(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "shared/flow.mh", `
export enum Level { discovery, delivery }
export type Req = { level: Level, artifact: string }
export workflow ArtifactFlow {
    description: "base flow"
    input level: Level
    input artifact: string
    var result = ""
    output: { result }
    step A {
        self.result = "${level}:${artifact}"
        if (!context.resumed) pause("review")
    }
}
export workflow TypedFlow(req: Req): {result: string} {
    output: { result: "${req.level}/${req.artifact}" }
    step A { log("x") }
}
`)
	writeFile(t, dir, "delivery.mh", `
import { ArtifactFlow, TypedFlow, Level } from "shared/flow.mh"
workflow Delivery = ArtifactFlow with { level: Level.delivery } {
    description: "Delivery artifacts"
}
workflow TypedDelivery = TypedFlow with { level: "delivery" }
`)
	writeFile(t, dir, "discovery.mh", `
import { ArtifactFlow } from "shared/flow.mh"
workflow Discovery = ArtifactFlow with { level: "discovery" }
`)
	wfs, err := execsvc.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, name := range []string{"ArtifactFlow", "TypedFlow", "Delivery", "TypedDelivery", "Discovery"} {
		if _, ok := wfs[name]; !ok {
			t.Fatalf("Load is missing %q (got %v)", name, keys(wfs))
		}
	}
	d := wfs["Delivery"]
	if d.Pipeline.Description != "Delivery artifacts" || wfs["Discovery"].Pipeline.Description != "base flow" {
		t.Errorf("descriptions = %q / %q", d.Pipeline.Description, wfs["Discovery"].Pipeline.Description)
	}
	props := d.Pipeline.InputSchema()["properties"].(map[string]any)
	if _, has := props["level"]; has || props["artifact"] == nil {
		t.Errorf("an alias's schema must drop its bound inputs, got %v", props)
	}

	run := func(w execsvc.Workflow, inputs map[string]any, resume bool) (*execsvc.Result, error) {
		return execsvc.Run(execsvc.Request{Source: w.File, Workflow: w.Name, Inputs: inputs, BaseDir: dir, Resume: resume})
	}
	if res, err := run(d, map[string]any{"artifact": "adr"}, false); err != nil || !res.Paused {
		t.Fatalf("Delivery first run: paused=%v err=%v", res != nil && res.Paused, err)
	}
	// The paused alias resumes under its own name; the other alias's runs are
	// separate sessions.
	if res, err := run(wfs["Discovery"], map[string]any{"artifact": "brief"}, false); err != nil || res.Vars["result"] != "discovery:brief" {
		t.Fatalf("Discovery: %v %v", res, err)
	}
	res, err := run(d, nil, true)
	if err != nil || res.Vars["result"] != "delivery:adr" {
		t.Fatalf("Delivery resume: vars=%v err=%v", res, err)
	}

	if _, err := run(d, map[string]any{"artifact": "adr", "level": "discovery"}, false); err == nil || !strings.Contains(err.Error(), `undeclared input "level"`) {
		t.Fatalf("a bound input must not be passable, got %v", err)
	}
	if res, err := run(wfs["TypedDelivery"], map[string]any{"artifact": "plano"}, false); err != nil || res.Vars["result"] != "delivery/plano" {
		t.Fatalf("an alias of a typed signature binds the field inside the param object: %v %v", res, err)
	}
}

func keys(m map[string]execsvc.Workflow) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// An `internal` workflow is not an entry point: Load does not publish it,
// `mhl run` (no name) skips it — or explains itself when it is all there is —
// while an alias of it still runs its steps.
func TestInternalWorkflowIsNotAnEntryPoint(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "main.mh", `
internal workflow Flow {
    input level: string
    var out = ""
    step A { self.out = level }
}
workflow Public = Flow with { level: "pub" }
`)
	wfs, err := execsvc.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, published := wfs["Flow"]; published || len(wfs) != 1 {
		t.Fatalf("Load published %v, want only Public", keys(wfs))
	}
	res, err := execsvc.Run(execsvc.Request{Source: src, BaseDir: dir})
	if err != nil || res.PipelineName != "Public" || res.Vars["out"] != "pub" {
		t.Fatalf("default run = %+v (err %v), want the Public alias", res, err)
	}

	only := writeFile(t, t.TempDir(), "only.mh", `internal workflow Flow { step A { log("x") } }`)
	if _, err := execsvc.Run(execsvc.Request{Source: only, BaseDir: dir}); err == nil || !strings.Contains(err.Error(), `workflow "Flow" is internal`) {
		t.Fatalf("running a file with only an internal workflow: %v", err)
	}
}

// Load resolves a module shared by several workflow files once: every
// workflow's Program holds the same declaration node for it. A library file
// that declares no pipeline/workflow is never resolved as an entry of its
// own — so one whose imports are broken but that no workflow imports does
// not fail the directory (`mhl lint` still reports it).
func TestLoadSharesImportedModulesAcrossWorkflows(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"lib/agents.mh": `export agent Shared { command: "echo" }`,
		"lib/unused.mh": `import "does-not-exist.mh"
export agent Unused { command: "echo" }`,
		"a.mh": `import {Shared} from "lib/agents.mh"
workflow A { step S { log("a") } }`,
		"b.mh": `import "lib/agents.mh"
workflow B { step S { log("b") } }`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	workflows, err := execsvc.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(workflows) != 2 {
		t.Fatalf("expected workflows A and B, got %+v", workflows)
	}
	shared := func(name string) any {
		for _, d := range workflows[name].Program.Decls {
			if d.Agent != nil && d.Agent.Name == "Shared" {
				return d.Agent
			}
		}
		t.Fatalf("workflow %s: agent Shared not merged in", name)
		return nil
	}
	if shared("A") != shared("B") {
		t.Error("expected A and B to share lib/agents.mh's declaration node")
	}
}

// ParseDir parses concurrently but stays deterministic: files come back in
// path order and, with several broken files, the error is the first one's.
func TestParseDirDeterministic(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"d", "a", "c", "b", "e"} {
		write(n+".mh", `pipeline P`+n+` { step S { log("x") } }`)
	}
	files, err := execsvc.ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	var got []string
	for _, f := range files {
		got = append(got, filepath.Base(f.Path))
	}
	if want := "a.mh b.mh c.mh d.mh e.mh"; strings.Join(got, " ") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}

	write("x_broken.mh", `pipeline {`)
	write("c_broken.mh", `pipeline {`)
	for i := 0; i < 20; i++ {
		_, err := execsvc.ParseDir(dir)
		if err == nil || !strings.Contains(err.Error(), "c_broken.mh") {
			t.Fatalf("err = %v, want the first broken file in path order (c_broken.mh)", err)
		}
	}
}

// LoadFiles resolves library modules from the ASTs ParseDir produced, not by
// reading them again: here the library file is gone from disk once parsed.
func TestLoadFilesUsesParsedLibraryModules(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.mh")
	if err := os.WriteFile(lib, []byte(`export agent Lib { command: "echo" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wf.mh"), []byte(`import {Lib} from "lib.mh"
workflow W { step S { log("w") } }`), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := execsvc.ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if err := os.Remove(lib); err != nil {
		t.Fatal(err)
	}
	workflows, err := execsvc.LoadFiles(dir, files)
	if err != nil {
		t.Fatalf("LoadFiles: %v", err)
	}
	if _, ok := workflows["W"]; !ok {
		t.Fatalf("expected W, got %+v", workflows)
	}
}
