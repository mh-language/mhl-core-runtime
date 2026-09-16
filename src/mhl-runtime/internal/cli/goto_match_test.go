package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

func TestRunGotoMatchDispatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `workflow W {
  step Dispatch {
    var artifact = "brief"
    goto match artifact {
      "brief" -> Brief
      "adr" -> Adr
      _ -> fail("unknown: " + artifact)
    }
  }
  step Skip { log("skip") }
  step Brief { log("brief reached") }
  step Adr { log("adr reached") }
}`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cli.Run([]string{"run", "main.mh"}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "step: Skip") || !strings.Contains(out.String(), "step: Brief") {
		t.Fatalf("wrong route: %s", out.String())
	}
}

func TestRunGotoMatchFallbackFails(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `workflow W {
  step Dispatch {
    var artifact = "unknown"
    goto match artifact {
      "brief" -> Brief
      _ -> fail("unknown artifact: " + artifact)
    }
  }
  step Brief {}
}`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := cli.Run([]string{"run", "main.mh"}, &out)
	if err == nil || !strings.Contains(err.Error(), "unknown artifact: unknown") {
		t.Fatalf("expected fallback failure, got %v; output: %s", err, out.String())
	}
}

func TestRunGotoMatchAfterResume(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	src := `workflow W {
  input artifact: string
  input approved: bool
  step Gate {
    if (!approved) pause("review")
    goto match artifact {
      "brief" -> Brief
      "adr" -> Adr
      _ -> fail("unknown artifact")
    }
  }
  step Skip {}
  step Brief { log("brief") }
  step Adr { log("adr") }
}`
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cli.Run([]string{"run", "main.mh", "--session", "goto-match-resume", "--input", "artifact=brief", "--input", "approved=false"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "paused") {
		t.Fatalf("expected pause: %s", out.String())
	}
	out.Reset()
	if err := cli.Run([]string{"run", "main.mh", "--session", "goto-match-resume", "--resume", "--input", "approved=true"}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "step: Skip") || !strings.Contains(out.String(), "step: Brief") {
		t.Fatalf("wrong route after resume: %s", out.String())
	}
}
