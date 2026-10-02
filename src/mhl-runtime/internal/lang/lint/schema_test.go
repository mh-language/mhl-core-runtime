package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func TestSchemaDeclarationSource(t *testing.T) {
	cases := []struct {
		name, file, content, want string
	}{
		{"valid object", "s.json", `{"type": "object"}`, ""},
		{"missing file", "", "", "no such file"},
		{"not an object", "s.json", `[1, 2]`, "must be a JSON object"},
		{"invalid json", "s.json", `{"type":`, "invalid JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.file != "" {
				if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			main := filepath.Join(dir, "main.mh")
			write(t, main, `
schema S from "s.json"
pipeline P { step A { log(S.path) } }
`)
			got := messages(lint.File(main))
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("unexpected findings: %q", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], tc.want) || !strings.Contains(got[0], `schema "S" from "s.json"`) {
				t.Fatalf("want one finding containing %q, got %q", tc.want, got)
			}
		})
	}
}
