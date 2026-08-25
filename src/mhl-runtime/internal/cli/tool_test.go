package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- tool method dispatch: positional binding, function-call scoping ------

func TestToolMethodPositionalBinding(t *testing.T) {
	out, err := run(t, `
tool execution {
    add(a, b) -> a + b
}

`+wrapStep(`
        log(execution.add(2, 3))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "5\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolMethodWrongArgCountErrors(t *testing.T) {
	_, err := run(t, `
tool execution {
    add(a, b) -> a + b
}

`+wrapStep(`
        log(execution.add(2))
    `))
	if err == nil || !strings.Contains(err.Error(), "requires 2 argument(s), got 1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolNotFoundErrors(t *testing.T) {
	_, err := run(t, wrapStep(`log(ghost.add(1, 2))`))
	if err == nil {
		t.Fatal("expected an error for an undeclared tool")
	}
}

func TestToolMethodNotFoundErrors(t *testing.T) {
	_, err := run(t, `
tool execution {
    add(a, b) -> a + b
}

`+wrapStep(`
        log(execution.missing())
    `))
	if err == nil || !strings.Contains(err.Error(), `tool "execution" has no method "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestToolMethodDoesNotSeeCallerVariables confirms a tool method call is a
// real function-call boundary: it only sees its own bound parameters, not
// the calling step's variables, even though both are plain identifiers.
func TestToolMethodDoesNotSeeCallerVariables(t *testing.T) {
	_, err := run(t, `
tool execution {
    echo_caller_var() -> caller_only
}

`+wrapStep(`
        var caller_only = 1
        log(execution.echo_caller_var())
    `))
	if err == nil || !strings.Contains(err.Error(), `undefined variable "caller_only"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLogWorksInsideToolMethodBody is the case that surfaced the gap:
// log(...) previously only worked as a top-level statement, so a tool
// method body like `print_json(json: any) -> log(json)` failed with
// "undefined variable log". log is now recognized inside the general
// expression evaluator itself (evalPostfix), not just at statement level.
func TestLogWorksInsideToolMethodBody(t *testing.T) {
	out, err := run(t, `
tool T {
    print_json(json) -> log(json)
}

`+wrapStep(`
        T.print_json({a: 1})
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `{"a":1}`) {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestLogAsExpressionValueIsNil(t *testing.T) {
	out, err := run(t, wrapStep(`
        var x = log("side effect")
        log(x)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "side effect\n") {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "null\n") {
		t.Errorf("expected log(...) to evaluate to null, got: %s", out)
	}
}

// --- cmd.exec --------------------------------------------------------

func TestToolCmdExec(t *testing.T) {
	out, err := run(t, `
tool execution {
    run_echo() -> cmd.exec("echo hi-from-tool", timeout: 5s)
}

`+wrapStep(`
        var res = execution.run_echo()
        log(res.exit_code)
        log(res.stdout)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "0\n") {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "hi-from-tool") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestToolCmdExecArgvArrayPreservesSpacesInAnArgument confirms cmd.exec's
// argv-array form (`cmd.exec([...])`) passes each element through verbatim
// — unlike the plain-string form, which splits on whitespace and would
// break an argument like a git commit message.
func TestToolCmdExecArgvArrayPreservesSpacesInAnArgument(t *testing.T) {
	out, err := run(t, wrapStep(`
        var res = cmd.exec(["echo", "-n", "hello world"])
        log(res.exit_code)
        log(res.stdout)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "0\n") {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "hello world\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolCmdExecArgvArrayNonStringElementFallsBackToArgError(t *testing.T) {
	_, err := run(t, wrapStep(`log(cmd.exec(["echo", 1]))`))
	if err == nil || !strings.Contains(err.Error(), "cmd.exec requires a string command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- cmd.exec_all ----------------------------------------------------

// TestToolCmdExecAllRunsMixedCommandForms exercises the
// Flows.Development TryParallelConfiguredVerify shape: several independent
// verify gates (lint/typecheck/tests) fanning out and getting AND'd
// together, mixing the plain-string and argv-array command forms in the
// same call — results stay in input order regardless of which finishes
// first.
func TestToolCmdExecAllRunsMixedCommandForms(t *testing.T) {
	out, err := run(t, wrapStep(`
        var results = cmd.exec_all(["echo lint-ok", ["echo", "typecheck ok"]], timeout: 5s)
        log(results.size())
        log(results[0].exit_code)
        log(results[0].stdout)
        log(results[1].exit_code)
        log(results[1].stdout)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "2\n") {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "lint-ok") {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "typecheck ok") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolCmdExecAllRequiresAnArrayArgument(t *testing.T) {
	_, err := run(t, wrapStep(`log(cmd.exec_all("not an array"))`))
	if err == nil || !strings.Contains(err.Error(), "cmd.exec_all requires an array of commands") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- json.stringify --------------------------------------------------------

func TestToolJSONStringifyThenParseRoundTrips(t *testing.T) {
	out, err := run(t, wrapStep(`
        var text = json.stringify({id: 1, title: "auth"})
        log(text)
        var back = json.parse(text)
        log(back.title)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `{"id":1,"title":"auth"}`) {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(out, "auth\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// --- json.parse_lines ------------------------------------------------------

// TestToolJSONParseLinesSkipsBlankAndInvalidLines proves json.parse_lines'
// forgiving-by-design behavior: unlike json.parse, a line that isn't valid
// JSON on its own doesn't fail the whole call — it's silently skipped,
// since a real NDJSON stream from an external CLI can interleave blank
// lines or plain log text mhl has no way to filter out beforehand.
func TestToolJSONParseLinesSkipsBlankAndInvalidLines(t *testing.T) {
	out, err := run(t, wrapStep(`
        var events = json.parse_lines("{\"a\":1}\n\nnot json\n{\"a\":2}\n")
        log(events.size())
        log(events[0].a)
        log(events[1].a)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "2\n1\n2\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestToolJSONParseLinesExtractsFinalCodexAgentMessage proves the pattern
// this project settled on after a real failure: `codex exec --json` streams
// one NDJSON event per line, and handing that whole multi-line blob to
// json.parse broke with "invalid character '{' after top-level value"
// (encoding/json only ever decodes the first top-level value). The runtime
// deliberately does NOT special-case Codex's (or Claude's) stream shape
// itself — that contract belongs to the CLI, not to mhl, and hard-coding it
// into the Go runtime would mean a new mhl release every time the CLI
// changes its event shape. Instead json.parse_lines is a generic building
// block, and the actual "which event/field is the real answer" rule is
// ordinary .mh code (here, a tool method) — inspectable and editable
// without recompiling anything. Mirrors a real handoff pipeline's
// CodexAgentAdapter.extractResult(...) tool method.
func TestToolJSONParseLinesExtractsFinalCodexAgentMessage(t *testing.T) {
	fixture, err := filepath.Abs("testdata/codex_json_stream_response.ndjson")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	out, err := run(t, `
tool CodexAgentAdapter {
    extractResult(raw) -> {
        var events = json.parse_lines(raw)
        var completed = events.filter((e) -> e.type == "item.completed")
        var agentMessages = completed.filter((e) -> e.item.type == "agent_message")
        return agentMessages[agentMessages.size() - 1].item.text
    }
}

`+wrapStep(`
        var raw = fs.read("`+filepath.ToSlash(fixture)+`")
        var text = CodexAgentAdapter.extractResult(raw)
        var parsed = json.parse(text)
        log("TARGET_DIR=${parsed.TARGET_DIR} VERIFY_CMD=${parsed.VERIFY_CMD}")
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := `TARGET_DIR=app/portable-agent-runtime VERIFY_CMD=GOCACHE="$PWD/.cache/go-build" go test ./...`
	if !strings.Contains(out, want) {
		t.Errorf("expected %q in output, got: %s", want, out)
	}
}

// --- git.add / git.commit / git.status / git.rev_parse / git.log ---------

// TestGitHandoffCycle exercises the same add→commit→status→rev_parse→log
// sequence Flows.Development's Handoff step relies on
// (dotnet/Flows.Development/DevelopmentTasks.Handoff.cs), through the
// language's native git namespace rather than nativeops directly — the
// commit message deliberately contains spaces, the case git.commit exists
// to handle safely.
func TestGitHandoffCycle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("config", "user.email", "t@t.com")
	runGit("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit("add", "f.txt")
	runGit("commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	out, err := run(t, wrapStep(`
        var added = git.add(["."])
        log(added.exit_code)

        var committed = git.commit("feat: complete feature #1 - with spaces")
        log(committed.exit_code)

        var status = git.status()
        log(status.stdout.is_empty())

        var hash = git.rev_parse("HEAD")
        log(hash.is_empty())

        var history = git.log(10)
        log(history)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "0\n0\n") {
		t.Errorf("expected both add and commit to exit 0, got: %s", out)
	}
	if !strings.Contains(out, "true\n") {
		t.Errorf("expected an empty status (clean tree) and a non-empty hash, got: %s", out)
	}
	if !strings.Contains(out, "with spaces") {
		t.Errorf("expected git.log to mention the commit message, got: %s", out)
	}
}

func TestGitCommitRequiresMessage(t *testing.T) {
	_, err := run(t, wrapStep(`log(git.commit())`))
	if err == nil || !strings.Contains(err.Error(), "git.commit requires a string message") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- fs.read / fs.write -------------------------------------------------

func TestToolFSWriteThenRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	out, err := run(t, `
tool execution {
    write_file(path, content) -> fs.write(path, content)
    read_file(path) -> fs.read(path)
}

pipeline P {
    step S {
        execution.write_file("`+filepath.ToSlash(path)+`", "written by tool")
        log(execution.read_file("`+filepath.ToSlash(path)+`"))
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "written by tool\n") {
		t.Errorf("unexpected output: %s", out)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(content) != "written by tool" {
		t.Errorf("file content = %q", content)
	}
}

// TestToolFSAppendAddsWithoutOverwriting is the case fs.append exists for:
// a progress.txt-style log where each write should add a line, not read
// the whole file back just to rewrite it with one more line tacked on.
func TestToolFSAppendAddsWithoutOverwriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress.txt")
	out, err := run(t, `
tool execution {
    append_line(path, line) -> fs.append(path, line)
    read_file(path) -> fs.read(path)
}

pipeline P {
    step S {
        execution.append_line("`+filepath.ToSlash(path)+`", "first\n")
        execution.append_line("`+filepath.ToSlash(path)+`", "second\n")
        log(execution.read_file("`+filepath.ToSlash(path)+`"))
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "first\nsecond\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolFSAppendCreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	out, err := run(t, wrapStep(`
        fs.append("`+filepath.ToSlash(path)+`", "hello")
        log(fs.read("`+filepath.ToSlash(path)+`"))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hello\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolFSReadMissingFileErrors(t *testing.T) {
	_, err := run(t, `
tool execution {
    read_file(path) -> fs.read(path)
}

`+wrapStep(`log(execution.read_file("/no/such/file/here.txt"))`))
	if err == nil {
		t.Fatal("expected an error reading a missing file")
	}
}

// --- fs.exists -------------------------------------------------------------

func TestToolFSExistsTrueForExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "present.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := run(t, wrapStep(`log(fs.exists("`+filepath.ToSlash(path)+`"))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "true\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolFSExistsFalseForMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.txt")
	out, err := run(t, wrapStep(`log(fs.exists("`+filepath.ToSlash(path)+`"))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "false\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

// --- fs.delete -------------------------------------------------------------

func TestToolFSDeleteRemovesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := run(t, wrapStep(`log(fs.delete("`+filepath.ToSlash(path)+`"))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "true\n") {
		t.Errorf("unexpected output: %s", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be gone, stat err = %v", err)
	}
}

func TestToolFSDeleteMissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.txt")
	_, err := run(t, wrapStep(`log(fs.delete("`+filepath.ToSlash(path)+`"))`))
	if err == nil {
		t.Fatal("expected an error deleting a missing file")
	}
}

// --- fs.list ---------------------------------------------------------------

func TestToolFSListReturnsEntryPaths(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	out, err := run(t, wrapStep(`
        var entries = fs.list("`+filepath.ToSlash(dir)+`")
        log(entries.size())
        for (var entry in entries) log(entry)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "2\n") {
		t.Errorf("expected 2 entries: %s", out)
	}
	for _, want := range []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")} {
		if !strings.Contains(out, filepath.ToSlash(want)) {
			t.Errorf("output missing entry %q: %s", want, out)
		}
	}
}

func TestToolFSListMissingDirErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent")
	_, err := run(t, wrapStep(`log(fs.list("`+filepath.ToSlash(path)+`"))`))
	if err == nil {
		t.Fatal("expected an error listing a missing directory")
	}
}

// --- fs.join -----------------------------------------------------------

func TestToolFSJoinCombinesSegments(t *testing.T) {
	out, err := run(t, wrapStep(`log(fs.join("a", "b", "c.txt"))`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, filepath.Join("a", "b", "c.txt")+"\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolFSJoinResultIsUsableByOtherFSOps(t *testing.T) {
	dir := t.TempDir()
	out, err := run(t, wrapStep(`
        var path = fs.join("`+filepath.ToSlash(dir)+`", "out.txt")
        fs.write(path, "hi")
        log(fs.read(path))
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hi\n") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestToolFSJoinRequiresAtLeastOneSegment(t *testing.T) {
	_, err := run(t, wrapStep(`log(fs.join())`))
	if err == nil {
		t.Fatal("expected an error joining zero segments")
	}
}

// --- http.post -----------------------------------------------------------

func TestToolHTTPPost(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		gotBody = buf.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	out, err := run(t, `
tool notifier {
    notify(url, message) -> http.post(
        url: url,
        headers: {"Content-Type": "application/json"},
        body: {"text": message}
    )
}

pipeline P {
    step S {
        var resp = notifier.notify("`+srv.URL+`", "hello there")
        log(resp.status)
        log(resp.body)
    }
}
`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "200\n") {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.Contains(gotBody, "hello there") {
		t.Errorf("server did not receive expected body: %s", gotBody)
	}
}

// --- native namespaces are reserved, not user-declarable ------------------

func TestNativeNamespaceUnknownOperationErrors(t *testing.T) {
	_, err := run(t, wrapStep(`log(cmd.frobnicate("x"))`))
	if err == nil || !strings.Contains(err.Error(), "not a supported native operation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNativeNamespaceChainedFieldAccess(t *testing.T) {
	out, err := run(t, wrapStep(`
        var res = cmd.exec("echo chained")
        log(res.exit_code)
    `))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "0\n") {
		t.Errorf("unexpected output: %s", out)
	}
}
