// Package cli implements the mhl command-line surface. This slice provides the
// `mhl skills list` command (IF-2), which inspects the .mhl files in a project
// and prints the declared Skills together with their scoped tools and
// mcp_servers.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yanjustino/mhl-runtime/internal/parser"
	"github.com/yanjustino/mhl-runtime/internal/runtime"
	"github.com/yanjustino/mhl-runtime/internal/skills"
)

// Run dispatches a mhl subcommand. It writes user-facing output to out and
// returns a non-nil error on failure.
func Run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl <command> [args]\n  commands: run, skills")
	}
	switch args[0] {
	case "skills":
		return runSkills(args[1:], out)
	case "run":
		return runPipeline(args[1:], out)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// runPipeline implements:
//
//	mhl run <pipeline.mhl> [--input key=value ...] [--resume]
//
// It executes a pipeline from the start, or resumes it from the last saved
// checkpoint when --resume is given (IF-1).
func runPipeline(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl run <pipeline.mhl> [--input key=value ...] [--resume]")
	}
	file := args[0]
	resume := false
	inputs := map[string]string{}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--resume":
			resume = true
		case "--input":
			if i+1 >= len(args) {
				return fmt.Errorf("--input requires a key=value argument")
			}
			i++
			k, v, ok := strings.Cut(args[i], "=")
			if !ok {
				return fmt.Errorf("--input expects key=value, got %q", args[i])
			}
			inputs[k] = v
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	src, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}
	prog, err := parser.Parse(string(src))
	if err != nil {
		return fmt.Errorf("parsing %s: %w", file, err)
	}
	pipeline, err := runtime.FindPipeline(prog, "")
	if err != nil {
		return err
	}

	runner := runtime.NewRunner(".")
	exec := func(step string, ctx *runtime.RunContext) error {
		for k, v := range inputs {
			ctx.Vars[k] = v
		}
		ctx.Vars["__last_step"] = step
		fmt.Fprintf(out, "step: %s\n", step)
		return nil
	}

	res, err := runner.Run(pipeline, exec, resume)
	if err != nil {
		return err
	}
	if res.Resumed {
		fmt.Fprintf(out, "resumed pipeline %q; skipped %d completed step(s)\n", pipeline.Name, len(res.Skipped))
	}
	fmt.Fprintf(out, "executed %d step(s)\n", len(res.Executed))
	return nil
}

func runSkills(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: mhl skills list [dir]")
	}
	dir := "."
	if len(args) > 1 {
		dir = args[1]
	}
	return skillsList(dir, out)
}

// skillsList scans dir for .mhl files, parses each, and prints every declared
// skill with its declared tools and mcp_servers.
func skillsList(dir string, out io.Writer) error {
	files, err := findMHLFiles(dir)
	if err != nil {
		return err
	}

	var defs []*skills.SkillDefinition
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("reading %s: %w", f, err)
		}
		prog, err := parser.Parse(string(src))
		if err != nil {
			return fmt.Errorf("parsing %s: %w", f, err)
		}
		defs = append(defs, skills.ListSkills(prog)...)
	}

	if len(defs) == 0 {
		fmt.Fprintln(out, "No skills declared in the current project.")
		return nil
	}

	for _, d := range defs {
		fmt.Fprintf(out, "Skill: %s\n", d.Name)
		if d.Description != "" {
			fmt.Fprintf(out, "  description: %s\n", d.Description)
		}
		fmt.Fprintf(out, "  tools: [%s]\n", strings.Join(d.Tools, ", "))
		fmt.Fprintf(out, "  mcp_servers: [%s]\n", strings.Join(d.MCPServers, ", "))
	}
	return nil
}

// findMHLFiles returns the sorted list of .mhl files under dir (recursively).
func findMHLFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".mhl") {
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
