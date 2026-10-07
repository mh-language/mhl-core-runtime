package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestFSReadEncodingFeedsAgentStdinAsUTF8 is the case `encoding:` exists
// for: a legacy Windows-1252 file read with fs.read and handed to an agent
// through `stdin:` must reach the subprocess as valid UTF-8 — a strict CLI
// (e.g. Devin) refuses anything else.
func TestFSReadEncodingFeedsAgentStdinAsUTF8(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/cat")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "legacy.txt")
	sink := filepath.Join(dir, "stdin.bin")
	if err := os.WriteFile(src, []byte("a\xe7\xe3o"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, `
agent Echo {
    command: "sh"
    args: ["-c", "tee `+filepath.ToSlash(sink)+`"]
    stdin: "${prompt}"
}
`+wrapStep(`
        var text = fs.read("`+filepath.ToSlash(src)+`", encoding: "windows-1252")
        log(Echo.run(prompt: text))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "ação\n") {
		t.Errorf("unexpected output: %s", out)
	}
	got, _ := os.ReadFile(sink)
	if !utf8.Valid(got) || !bytes.Contains(got, []byte("ação")) {
		t.Errorf("agent stdin = % x, want valid UTF-8 \"ação\"", got)
	}
}

func TestFSWriteAndAppendWithEncoding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	out, err := run(t, wrapStep(`
        fs.write("`+filepath.ToSlash(path)+`", "nome;ação\n", "utf-16")
        fs.append("`+filepath.ToSlash(path)+`", "x;ç\n", encoding: "utf-16")
        log(fs.read("`+filepath.ToSlash(path)+`", "auto"))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "nome;ação\nx;ç\n") {
		t.Errorf("unexpected output: %s", out)
	}
	raw, _ := os.ReadFile(path)
	if !bytes.HasPrefix(raw, []byte{0xFF, 0xFE}) || bytes.Count(raw, []byte{0xFF, 0xFE}) != 1 {
		t.Errorf("want a single UTF-16LE BOM at the start, got % x", raw)
	}
}

func TestFSReadUnknownEncodingErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := run(t, wrapStep(`
        log(fs.read("`+filepath.ToSlash(path)+`", encoding: "klingon"))
    `))
	if err == nil || !strings.Contains(err.Error(), `unsupported encoding "klingon"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestCmdExecDecodesOutputEncoding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses printf")
	}
	out, err := run(t, wrapStep(`
        var r = cmd.exec(["printf", "a\\347\\343o"], encoding: "latin1")
        log(r.stdout)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "ação\n") {
		t.Errorf("unexpected output: %s", out)
	}
}
