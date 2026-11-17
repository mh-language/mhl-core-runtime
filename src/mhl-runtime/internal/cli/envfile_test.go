package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

const envFileWorkflow = `
workflow Greet {
    step Say {
        log("greeting=${env("MHL_ENVFILE_GREETING", "unset")} target=${env("MHL_ENVFILE_TARGET", "unset")}")
    }
}
`

// unsetEnv unsets key for the test and restores its previous state after.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	os.Unsetenv(key)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// `mhl run` loads the working directory's .env; a variable already in the
// environment wins over it.
func TestRunLoadsDotEnvFromWorkingDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, "main.mh", envFileWorkflow)
	writeFile(t, ".env", "# keys\nMHL_ENVFILE_GREETING=\"hello from .env\"\nMHL_ENVFILE_TARGET=dotenv\n")
	unsetEnv(t, "MHL_ENVFILE_GREETING")
	t.Setenv("MHL_ENVFILE_TARGET", "shell")

	var out bytes.Buffer
	if err := cli.Run([]string{"run", "main.mh"}, &out); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "greeting=hello from .env target=shell") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// --env-file replaces the default .env; a later file overrides an earlier one.
func TestRunEnvFileFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, "main.mh", envFileWorkflow)
	writeFile(t, ".env", "MHL_ENVFILE_GREETING=ignored\n")
	writeFile(t, "base.env", "MHL_ENVFILE_GREETING=base\nMHL_ENVFILE_TARGET=base\n")
	writeFile(t, "local.env", "MHL_ENVFILE_TARGET=local\n")
	unsetEnv(t, "MHL_ENVFILE_GREETING")
	unsetEnv(t, "MHL_ENVFILE_TARGET")

	var out bytes.Buffer
	args := []string{"run", "--env-file", "base.env", "main.mh", "--env-file", filepath.Join(dir, "local.env")}
	if err := cli.Run(args, &out); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "greeting=base target=local") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestRunEnvFileErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, "main.mh", envFileWorkflow)

	cases := []struct {
		name string
		env  string // the .env content; "" = none
		args []string
		want string
	}{
		{"missing --env-file", "", []string{"run", "main.mh", "--env-file", "nope.env"}, "nope.env"},
		{"--env-file without a path", "", []string{"run", "main.mh", "--env-file"}, "--env-file requires a path"},
		{"malformed .env", "MHL_ENVFILE_OK=1\nnot a pair\n", []string{"run", "main.mh"}, ".env:2: expected KEY=VALUE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(".env")
			if c.env != "" {
				writeFile(t, ".env", c.env)
			}
			err := cli.Run(c.args, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error containing %q, got %v", c.want, err)
			}
		})
	}
}

// `mhl test` loads the same .env, so a test block sees the run's settings.
func TestTestLoadsDotEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, "main.mh", `
test dotenv {
    describe env_file_value {
        are_equal(env("MHL_ENVFILE_GREETING", "unset"), "hi")
    }
}
`)
	writeFile(t, "ci.env", "MHL_ENVFILE_GREETING=hi\n")
	unsetEnv(t, "MHL_ENVFILE_GREETING")

	var out bytes.Buffer
	if err := cli.Run([]string{"test", "main.mh", "--env-file", "ci.env"}, &out); err != nil {
		t.Fatalf("test: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "PASS") {
		t.Fatalf("expected a passing assertion:\n%s", out.String())
	}

	// Without the file the assertion fails: the value really came from it.
	unsetEnv(t, "MHL_ENVFILE_GREETING")
	if err := cli.Run([]string{"test", "main.mh"}, &bytes.Buffer{}); err == nil {
		t.Fatal("without --env-file the assertion should fail")
	}
}
