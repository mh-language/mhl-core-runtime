package nativeops_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yanjustino/mhl-runtime/internal/features/nativeops"
)

func TestExecCapturesOutputAndExitCode(t *testing.T) {
	result, err := nativeops.Exec(context.Background(), "echo hello", 0)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result["exit_code"] != 0.0 {
		t.Errorf("exit_code = %v, want 0", result["exit_code"])
	}
	if !strings.Contains(result["stdout"].(string), "hello") {
		t.Errorf("stdout = %q", result["stdout"])
	}
}

// TestExecNonZeroExitIsNotAnError confirms cmd.exec's philosophy: a
// non-zero exit is a normal, inspectable outcome (result["exit_code"]),
// not a Go error — matching how an agent.run caller never sees a raw
// exec.ExitError either.
func TestExecNonZeroExitIsNotAnError(t *testing.T) {
	result, err := nativeops.Exec(context.Background(), "false", 0)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result["exit_code"] == 0.0 {
		t.Errorf("exit_code = %v, want non-zero", result["exit_code"])
	}
}

func TestExecBadBinaryErrors(t *testing.T) {
	_, err := nativeops.Exec(context.Background(), "definitely-not-a-real-binary-xyz", 0)
	if err == nil {
		t.Fatal("expected an error for a binary that does not exist")
	}
}

func TestExecEmptyCommandErrors(t *testing.T) {
	_, err := nativeops.Exec(context.Background(), "   ", 0)
	if err == nil || !strings.Contains(err.Error(), "command must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecTimeoutErrors(t *testing.T) {
	started := time.Now()
	_, err := nativeops.Exec(context.Background(), "sleep 30", 20*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("Exec took too long to return after timeout: %v", time.Since(started))
	}
}

// TestExecArgsPreservesSpacesInASingleArgument is the case Exec's
// whitespace-splitting cannot handle: a single argument (e.g. a git commit
// message) containing spaces must survive as one argv element.
func TestExecArgsPreservesSpacesInASingleArgument(t *testing.T) {
	result, err := nativeops.ExecArgs(context.Background(), []string{"echo", "-n", "hello world"}, 0)
	if err != nil {
		t.Fatalf("ExecArgs: %v", err)
	}
	if result["stdout"] != "hello world" {
		t.Errorf("stdout = %q, want %q", result["stdout"], "hello world")
	}
	if result["exit_code"] != 0.0 {
		t.Errorf("exit_code = %v, want 0", result["exit_code"])
	}
}

func TestExecArgsNonZeroExitIsNotAnError(t *testing.T) {
	result, err := nativeops.ExecArgs(context.Background(), []string{"false"}, 0)
	if err != nil {
		t.Fatalf("ExecArgs: %v", err)
	}
	if result["exit_code"] == 0.0 {
		t.Errorf("exit_code = %v, want non-zero", result["exit_code"])
	}
}

func TestExecArgsEmptyArgvErrors(t *testing.T) {
	_, err := nativeops.ExecArgs(context.Background(), nil, 0)
	if err == nil || !strings.Contains(err.Error(), "argv must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecArgsTimeoutErrors(t *testing.T) {
	started := time.Now()
	_, err := nativeops.ExecArgs(context.Background(), []string{"sleep", "30"}, 20*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("ExecArgs took too long to return after timeout: %v", time.Since(started))
	}
}

// TestExecAllRunsCommandsConcurrently is the reason ExecAll exists over a
// sequential loop of ExecArgs calls: three 300ms sleeps must finish in
// well under 900ms (their sum) if they truly ran in parallel, not one
// after another.
func TestExecAllRunsCommandsConcurrently(t *testing.T) {
	started := time.Now()
	results, err := nativeops.ExecAll(context.Background(), [][]string{
		{"sleep", "0.3"},
		{"sleep", "0.3"},
		{"sleep", "0.3"},
	}, 0)
	if err != nil {
		t.Fatalf("ExecAll: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed > 800*time.Millisecond {
		t.Errorf("ExecAll took %v, expected the three sleeps to overlap (well under their 900ms sum)", elapsed)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for i, r := range results {
		if r["exit_code"] != 0.0 {
			t.Errorf("results[%d].exit_code = %v, want 0", i, r["exit_code"])
		}
	}
}

// TestExecAllPreservesInputOrderNotCompletionOrder confirms results[i]
// always corresponds to commands[i], even when a later command finishes
// before an earlier one.
func TestExecAllPreservesInputOrderNotCompletionOrder(t *testing.T) {
	results, err := nativeops.ExecAll(context.Background(), [][]string{
		{"sh", "-c", "sleep 0.2; echo first"},
		{"echo", "second"},
	}, 0)
	if err != nil {
		t.Fatalf("ExecAll: %v", err)
	}
	if !strings.Contains(results[0]["stdout"].(string), "first") {
		t.Errorf("results[0].stdout = %q, want it to contain %q", results[0]["stdout"], "first")
	}
	if !strings.Contains(results[1]["stdout"].(string), "second") {
		t.Errorf("results[1].stdout = %q, want it to contain %q", results[1]["stdout"], "second")
	}
}

func TestExecAllNonZeroExitIsNotAnError(t *testing.T) {
	results, err := nativeops.ExecAll(context.Background(), [][]string{
		{"true"},
		{"false"},
	}, 0)
	if err != nil {
		t.Fatalf("ExecAll: %v", err)
	}
	if results[0]["exit_code"] != 0.0 {
		t.Errorf("results[0].exit_code = %v, want 0", results[0]["exit_code"])
	}
	if results[1]["exit_code"] == 0.0 {
		t.Errorf("results[1].exit_code = 0, want non-zero")
	}
}

func TestExecAllBadBinaryAbortsTheWholeBatch(t *testing.T) {
	_, err := nativeops.ExecAll(context.Background(), [][]string{
		{"echo", "ok"},
		{"definitely-not-a-real-binary-xyz"},
	}, 0)
	if err == nil {
		t.Fatal("expected an error for a binary that does not exist")
	}
	if !strings.Contains(err.Error(), "cmd.exec_all[1]") {
		t.Errorf("expected the error to identify the failing index, got: %v", err)
	}
}

func TestExecAllEmptyCommandsReturnsEmptyResults(t *testing.T) {
	results, err := nativeops.ExecAll(context.Background(), nil, 0)
	if err != nil {
		t.Fatalf("ExecAll: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}
