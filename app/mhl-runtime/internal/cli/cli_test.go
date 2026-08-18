package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/cli"
)

const projectSource = `
export skill CodeAuditorSkill {
    description: "Analisa código."
    tools: [execution.read_file, execution.git_diff]
    mcp_servers: [PostgresDB]
    system_instructions: """
    Auditor.
    """
}
`

// IF-2: `mhl skills list` prints declared skills with their tools/mcp_servers.
func TestSkillsList(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "project.mhl"), []byte(projectSource), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var buf bytes.Buffer
	if err := cli.Run([]string{"skills", "list", dir}, &buf); err != nil {
		t.Fatalf("skills list: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"Skill: CodeAuditorSkill",
		"execution.read_file",
		"execution.git_diff",
		"PostgresDB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("skills list output missing %q:\n%s", want, out)
		}
	}
}

func TestSkillsListEmpty(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := cli.Run([]string{"skills", "list", dir}, &buf); err != nil {
		t.Fatalf("skills list: %v", err)
	}
	if !strings.Contains(buf.String(), "No skills declared") {
		t.Errorf("expected empty message, got: %s", buf.String())
	}
}
