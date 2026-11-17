package dotenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Var
	}{
		{"plain", "A=1\nB=two words", []Var{{"A", "1"}, {"B", "two words"}}},
		{"comments and blanks", "# top\n\nA=1 # trailing\n  # indented\nB=x#y", []Var{{"A", "1"}, {"B", "x#y"}}},
		{"export prefix", "export A=1\nexported=2", []Var{{"A", "1"}, {"exported", "2"}}},
		{"empty value", "A=\nB=\"\"", []Var{{"A", ""}, {"B", ""}}},
		{"spaces around =", "A = 1", []Var{{"A", "1"}}},
		{"double quotes escape", `A="a\nb \"q\" \\ \$"`, []Var{{"A", "a\nb \"q\" \\ \\$"}}},
		{"double quotes keep #", `A="x # y" # c`, []Var{{"A", "x # y"}}},
		{"single quotes literal", `A='$HOME \n # x'`, []Var{{"A", `$HOME \n # x`}}},
		{"value with =", "URL=https://x.io/?a=b", []Var{{"URL", "https://x.io/?a=b"}}},
		{"bom and crlf", "\ufeffA=1\r\nB=2\r\n", []Var{{"A", "1"}, {"B", "2"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(c.in), ".env")
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no equals":    "JUSTAKEY",
		"bad name":     "1A=x",
		"dash in name": "MY-KEY=x",
		"open double":  `A="abc`,
		"open single":  `A='abc`,
		"junk after":   `A="abc" def`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(strings.NewReader("OK=1\n"+in), "x.env")
			if err == nil || !strings.HasPrefix(err.Error(), "x.env:2: ") {
				t.Fatalf("want an error at x.env:2, got %v", err)
			}
		})
	}
}

// unsetForTest unsets key for the test and restores its state afterwards.
func unsetForTest(t *testing.T, key string) {
	t.Setenv(key, "")
	os.Unsetenv(key)
}

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.env")
	b := filepath.Join(dir, "b.env")
	os.WriteFile(a, []byte("MHL_DOTENV_ONE=from-a\nMHL_DOTENV_TWO=from-a\nMHL_DOTENV_SHELL=from-a\n"), 0o600)
	os.WriteFile(b, []byte("MHL_DOTENV_TWO=from-b\n"), 0o600)
	unsetForTest(t, "MHL_DOTENV_ONE")
	unsetForTest(t, "MHL_DOTENV_TWO")
	t.Setenv("MHL_DOTENV_SHELL", "from-shell")

	set, err := Load([]string{a, b}, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(set, []string{"MHL_DOTENV_ONE", "MHL_DOTENV_TWO"}) {
		t.Fatalf("set = %v", set)
	}
	for k, want := range map[string]string{"MHL_DOTENV_ONE": "from-a", "MHL_DOTENV_TWO": "from-b", "MHL_DOTENV_SHELL": "from-shell"} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.env")
	if _, err := Load([]string{missing}, true); err != nil {
		t.Fatalf("optional missing file: %v", err)
	}
	if _, err := Load([]string{missing}, false); err == nil {
		t.Fatal("required missing file: want an error")
	}
}
