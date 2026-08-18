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
	"github.com/yanjustino/mhl-runtime/internal/skills"
)

// Run dispatches a mhl subcommand. It writes user-facing output to out and
// returns a non-nil error on failure.
func Run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl <command> [args]\n  commands: skills")
	}
	switch args[0] {
	case "skills":
		return runSkills(args[1:], out)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
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
