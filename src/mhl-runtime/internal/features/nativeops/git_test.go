package nativeops_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

// initGitRepo creates a throwaway git repo with one committed file, then an
// uncommitted change to it, so `git diff` has real, deterministic output.
func initGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", "f.txt")
	run("commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestDiffReturnsChangedContent(t *testing.T) {
	dir := initGitRepo(t)
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	diff, err := nativeops.Diff(context.Background(), "", "")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "v2") {
		t.Errorf("diff = %q, want it to mention the changed content", diff)
	}
}

func TestDiffOutsideRepoErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	_, err := nativeops.Diff(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected an error running git diff outside a repository")
	}
}

// TestHandoffCycleAddCommitStatusRevParse replicates Flows.Development's
// Handoff step (dotnet/Flows.Development/DevelopmentTasks.Handoff.cs): stage
// everything, commit with a message containing spaces (the case Fase 0.2's
// cmd.exec argv form exists for), confirm the working tree is clean, then
// read back the short commit hash.
func TestHandoffCycleAddCommitStatusRevParse(t *testing.T) {
	dir := initGitRepo(t)
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	add, err := nativeops.Add(context.Background(), []string{"."}, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if add["exit_code"] != 0.0 {
		t.Fatalf("Add exit_code = %v, stderr = %v", add["exit_code"], add["stderr"])
	}

	commit, err := nativeops.Commit(context.Background(), "feat(development): complete feature #1 - with spaces", nil, "")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if commit["exit_code"] != 0.0 {
		t.Fatalf("Commit exit_code = %v, stderr = %v", commit["exit_code"], commit["stderr"])
	}

	status, err := nativeops.Status(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if strings.TrimSpace(status["stdout"].(string)) != "" {
		t.Errorf("working tree should be clean after commit, got status: %q", status["stdout"])
	}

	hash, err := nativeops.RevParse(context.Background(), "HEAD", "")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if strings.TrimSpace(hash) == "" {
		t.Error("RevParse returned an empty hash")
	}

	log, err := nativeops.Log(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if !strings.Contains(log, "with spaces") {
		t.Errorf("log = %q, want it to mention the commit message", log)
	}
}

func TestAddNoPathsErrors(t *testing.T) {
	_, err := nativeops.Add(context.Background(), nil, "")
	if err == nil || !strings.Contains(err.Error(), "at least one path is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCommitEmptyMessageErrors(t *testing.T) {
	_, err := nativeops.Commit(context.Background(), "  ", nil, "")
	if err == nil || !strings.Contains(err.Error(), "message must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCommitNothingStagedIsNotAnError(t *testing.T) {
	dir := initGitRepo(t)
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// f.txt has an unstaged (not added) change from initGitRepo; nothing is
	// staged, so `git commit` exits non-zero — that is a normal, inspectable
	// outcome (exit_code), not a Go error.
	commit, err := nativeops.Commit(context.Background(), "empty commit attempt", nil, "")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if commit["exit_code"] == 0.0 {
		t.Error("exit_code = 0, want non-zero (nothing was staged)")
	}
}

func TestRevParseEmptyRefErrors(t *testing.T) {
	_, err := nativeops.RevParse(context.Background(), "", "")
	if err == nil || !strings.Contains(err.Error(), "ref must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRevParseUnknownRefErrors(t *testing.T) {
	dir := initGitRepo(t)
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	_, err := nativeops.RevParse(context.Background(), "not-a-real-ref", "")
	if err == nil {
		t.Fatal("expected an error for an unknown ref")
	}
}

func TestLogNonPositiveNErrors(t *testing.T) {
	_, err := nativeops.Log(context.Background(), 0, "")
	if err == nil || !strings.Contains(err.Error(), "n must be positive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestGitOpsRunAgainstDirArgument exercises the `dir` parameter: every op
// targets a repo the process has NOT chdir'd into, the shape a pipeline's
// Handoff step needs (it works on a checkout under some target_dir, not the
// interpreter's own working directory).
func TestGitOpsRunAgainstDirArgument(t *testing.T) {
	repo := initGitRepo(t)

	// The process CWD is the test binary's dir (a real repo, but not `repo`)
	// — proof the ops below act on `repo` only because `dir` says so.
	diff, err := nativeops.Diff(context.Background(), "", repo)
	if err != nil {
		t.Fatalf("Diff(dir): %v", err)
	}
	if !strings.Contains(diff, "v2") {
		t.Errorf("Diff(dir) = %q, want the change under %s", diff, repo)
	}

	add, err := nativeops.Add(context.Background(), []string{"."}, repo)
	if err != nil || add["exit_code"] != 0.0 {
		t.Fatalf("Add(dir): err=%v exit_code=%v stderr=%v", err, add["exit_code"], add["stderr"])
	}

	commit, err := nativeops.Commit(context.Background(), "commit via dir arg", nil, repo)
	if err != nil || commit["exit_code"] != 0.0 {
		t.Fatalf("Commit(dir): err=%v exit_code=%v stderr=%v", err, commit["exit_code"], commit["stderr"])
	}

	status, err := nativeops.Status(context.Background(), nil, repo)
	if err != nil {
		t.Fatalf("Status(dir): %v", err)
	}
	if strings.TrimSpace(status["stdout"].(string)) != "" {
		t.Errorf("Status(dir) = %q, want a clean tree after commit", status["stdout"])
	}

	hash, err := nativeops.RevParse(context.Background(), "HEAD", repo)
	if err != nil || strings.TrimSpace(hash) == "" {
		t.Fatalf("RevParse(dir): hash=%q err=%v", hash, err)
	}

	logOut, err := nativeops.Log(context.Background(), 5, repo)
	if err != nil || !strings.Contains(logOut, "commit via dir arg") {
		t.Fatalf("Log(dir) = %q, err=%v", logOut, err)
	}
}

func TestGitDirArgumentOutsideRepoIsInspectable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}
	notARepo := t.TempDir()

	// Status against a non-repo directory: a normal, inspectable non-zero
	// exit (the check a Handoff step does before deciding to commit), not a
	// Go error.
	status, err := nativeops.Status(context.Background(), nil, notARepo)
	if err != nil {
		t.Fatalf("Status(dir) outside a repo should not error: %v", err)
	}
	if status["exit_code"] == 0.0 {
		t.Errorf("exit_code = 0, want non-zero for a non-repo directory")
	}
}
