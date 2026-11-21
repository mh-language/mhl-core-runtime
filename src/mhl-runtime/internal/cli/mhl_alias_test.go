package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// ".mhl" is an alias of ".mh": a directory scan (`mhl lint`, `mhl test`)
// picks it up, and a .mh file can import a .mhl one and vice versa.
func TestLintDirFindsMhlFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.mhl"), []byte(`
pipeline P {
    step S {
        var response = Ghost.run(prompt: "hi")
    }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := cli.Run([]string{"lint", dir}, &buf); err == nil {
		t.Fatalf("expected the .mhl file's finding to fail lint, got:\n%s", buf.String())
	}
	if out := buf.String(); !strings.Contains(out, `agent "Ghost" not found`) || !strings.Contains(out, "main.mhl") {
		t.Errorf("output missing the main.mhl finding:\n%s", out)
	}
}

func TestTestDirMixesMhAndMhlFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"lib.mhl": `
export tool Greetings {
    greet(name: string) -> "Hello, " + name + "!"
}
`,
		"main.mh": `
import {Greetings} from "lib.mhl"

test t {
    describe from_mhl {
        are_equal(Greetings.greet("mh"), "Hello, mh!")
    }
}
`,
		"other.mhl": `
import {Greetings} from "lib.mhl"

test u {
    describe from_mhl_too {
        are_equal(Greetings.greet("mhl"), "Hello, mhl!")
    }
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := cli.Run([]string{"test", dir}, &buf); err != nil {
		t.Fatalf("test: %v\noutput:\n%s", err, buf.String())
	}
	if out := buf.String(); !strings.Contains(out, "2 passed, 0 failed") {
		t.Fatalf("expected both the .mh and the .mhl test to run:\n%s", out)
	}
}
