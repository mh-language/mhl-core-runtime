package cli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// cwdFixture makes a directory holding a marker file and, on Unix, an
// executable ./hello.sh — what a `cmd.exec(..., cwd: dir)` call should see
// only when it really starts there.
func cwdFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		script := "#!/bin/sh\necho hello-from-script\n"
		if err := os.WriteFile(filepath.Join(dir, "hello.sh"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCmdExecCwdRunsTheCommandInThatDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses ls and a shell script")
	}
	dir := cwdFixture(t)
	out, err := run(t, wrapStep(`
        var argv = cmd.exec(["ls"], cwd: "`+dir+`")
        log(argv.stdout)
        var text = cmd.exec("ls", cwd: "`+dir+`", timeout: 5s)
        log(text.stdout)
        var script = cmd.exec(["./hello.sh"], cwd: "`+dir+`")
        log(script.stdout)
        var inherited = cmd.exec(["ls"], cwd: "")
        log(inherited.stdout.contains("marker.txt"))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Count(out, "marker.txt") != 2 {
		t.Errorf("argv and string forms should both list marker.txt:\n%s", out)
	}
	if !strings.Contains(out, "hello-from-script") {
		t.Errorf("a relative command path should resolve under cwd:\n%s", out)
	}
	if !strings.Contains(out, "false\n") {
		t.Errorf(`cwd: "" should inherit mhl's working directory:`+"\n%s", out)
	}
}

func TestCmdExecAllCwdAppliesToEveryCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses ls and cat")
	}
	dir := cwdFixture(t)
	out, err := run(t, wrapStep(`
        var results = cmd.exec_all([["ls"], "cat marker.txt"], cwd: "`+dir+`")
        log(results[0].stdout.contains("marker.txt"))
        log(results[1].stdout)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "true\n") || !strings.Contains(out, "here") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestCmdExecCwdIsValidatedBeforeTheCommandStarts(t *testing.T) {
	dir := cwdFixture(t)
	cases := []struct {
		name, call, want string
	}{
		{"missing", `cmd.exec(["echo", "x"], cwd: "` + filepath.ToSlash(filepath.Join(dir, "nope")) + `")`, "cmd.exec: cwd not found"},
		{"file", `cmd.exec(["echo", "x"], cwd: "` + filepath.ToSlash(filepath.Join(dir, "marker.txt")) + `")`, "cmd.exec: cwd is not a directory"},
		{"not a string", `cmd.exec(["echo", "x"], cwd: 1)`, "cmd.exec: cwd must be a string"},
		{"exec_all", `cmd.exec_all([["echo", "x"]], cwd: "` + filepath.ToSlash(filepath.Join(dir, "nope")) + `")`, "cmd.exec_all: cwd not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := run(t, wrapStep(`log(`+tc.call+`)`))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}
