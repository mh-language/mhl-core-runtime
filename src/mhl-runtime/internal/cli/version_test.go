package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// TestVersionCommandAndAliasesPrintVersion proves `mhl version`, `mhl
// --version`, and `mhl -v` all report cli.Version — the value the
// Makefile's build/release targets inject via -ldflags -X at link time
// (see Makefile's VERSION/LDFLAGS), defaulting to "dev" otherwise.
func TestVersionCommandAndAliasesPrintVersion(t *testing.T) {
	original := cli.Version
	cli.Version = "v1.2.3-test"
	defer func() { cli.Version = original }()

	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		var buf bytes.Buffer
		if err := cli.Run(args, &buf); err != nil {
			t.Fatalf("cli.Run(%v): %v", args, err)
		}
		if !strings.Contains(buf.String(), "v1.2.3-test") {
			t.Errorf("cli.Run(%v) output = %q, want it to contain the version", args, buf.String())
		}
	}
}

// TestNoArgsUsageMentionsVersion proves the bare usage/help text lists
// `version` alongside the other commands, so it's discoverable without
// already knowing the flag exists.
func TestNoArgsUsageMentionsVersion(t *testing.T) {
	err := cli.Run(nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected a usage error for no arguments, got nil")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("usage error should mention the version command, got: %v", err)
	}
}
