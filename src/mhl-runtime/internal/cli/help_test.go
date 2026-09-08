package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// TestHelpCommandAndAliasesListEveryCommand proves `mhl help`, `mhl -h`, and
// `mhl --help` all print the ASCII logo and name every subcommand, so the
// command surface is discoverable without reading the source.
func TestHelpCommandAndAliasesListEveryCommand(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var buf bytes.Buffer
		if err := cli.Run(args, &buf); err != nil {
			t.Fatalf("cli.Run(%v): %v", args, err)
		}
		got := buf.String()
		for _, want := range []string{
			"Meta-Harness Language",
			"init", "run", "test", "lint", "lsp", "serve", "extension", "version",
			"--dry-run", "--resume",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("cli.Run(%v) output missing %q:\n%s", args, want, got)
			}
		}
	}
}

// TestPerCommandHelp proves `mhl <command> -h` / `--help` prints that
// command's focused usage (not the top-level help) and never runs anything.
func TestPerCommandHelp(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"init", "-h"}, "mhl init [dir]"},
		{[]string{"run", "--help"}, "mhl run <file.mh> [flags]"},
		{[]string{"run", "main.mh", "--help"}, "mhl run <file.mh> [flags]"},
		{[]string{"test", "-h"}, "mhl test <file.mh|dir>"},
		{[]string{"lint", "--help"}, "mhl lint [dir]"},
		{[]string{"lsp", "-h"}, "mhl lsp"},
		{[]string{"serve", "-h"}, "mhl serve <mcp|a2a>"},
		{[]string{"serve", "mcp", "-h"}, "mhl serve mcp --http"},
		{[]string{"serve", "a2a", "--help"}, "mhl serve a2a [flags]"},
		{[]string{"extension", "-h"}, "mhl extension <subcommand>"},
		{[]string{"extension", "install", "--help"}, "mhl extension <subcommand>"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		if err := cli.Run(tc.args, &buf); err != nil {
			t.Errorf("cli.Run(%v): %v", tc.args, err)
			continue
		}
		if !strings.Contains(buf.String(), tc.want) {
			t.Errorf("cli.Run(%v) output missing %q:\n%s", tc.args, tc.want, buf.String())
		}
	}
}

// TestNoArgsUsagePointsAtHelp proves the bare usage error tells the reader
// how to get the full help.
func TestNoArgsUsagePointsAtHelp(t *testing.T) {
	err := cli.Run(nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected a usage error for no arguments, got nil")
	}
	if !strings.Contains(err.Error(), "help") {
		t.Errorf("usage error should mention help, got: %v", err)
	}
}
