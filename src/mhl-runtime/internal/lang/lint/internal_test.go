package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func TestInternalToolMethodAccess(t *testing.T) {
	dir := t.TempDir()
	lib := `
export tool Paths {
    root(id: string): string -> self.clean(id)
    all(ids: string[]): string[] -> ids.map((i) -> Paths.clean(i))
    internal clean(id: string): string -> id.trim()
}
test LibSpec { describe d { are_equal(Paths.clean(" a "), "a") } }
`
	if err := os.WriteFile(filepath.Join(dir, "lib.mh"), []byte(lib), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := messages(lint.File(filepath.Join(dir, "lib.mh"))); len(got) != 0 {
		t.Fatalf("the tool itself and its own file's tests may call internals, got %q", got)
	}
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
import { Paths } from "lib.mh"
workflow W {
    input id: string
    step A {
        var ok = Paths.root(id)
        var bad = Paths.clean(id)
        log(json.stringify([id].map((x) -> Paths.clean(x))))
    }
}
test MainSpec { describe d { var r = Paths.clean("x") } }
`)
	got := messages(lint.File(main))
	if len(got) != 3 {
		t.Fatalf("want 3 findings, got %d: %q", len(got), got)
	}
	for _, m := range got {
		if !strings.Contains(m, "Paths.clean is internal to tool Paths") {
			t.Errorf("unexpected finding %q", m)
		}
	}
}
