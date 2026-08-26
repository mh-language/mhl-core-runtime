package tools_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/tools"
)

func TestExecCapturesOutput(t *testing.T) {
	name, args := "sh", []string{"-c", "printf hello"}
	if runtime.GOOS == "windows" {
		name, args = "cmd", []string{"/C", "echo hello"}
	}
	result, err := tools.Exec(context.Background(), name, args...)
	if err != nil || result.ExitCode != 0 || result.Stdout == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestExecCancellationReturnsPromptly(t *testing.T) {
	name, args := "sh", []string{"-c", "sleep 30"}
	if runtime.GOOS == "windows" {
		t.Skip("shell process fixture is POSIX-specific")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := tools.Exec(ctx, name, args...)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("cancellation err=%v duration=%v", err, time.Since(started))
	}
}
