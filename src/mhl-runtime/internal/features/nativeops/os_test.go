package nativeops_test

import (
	"os"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

func TestUser(t *testing.T) {
	got, err := nativeops.User()
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	if got == "" {
		t.Error("User() = \"\", want a non-empty account name")
	}
}

func TestHomeDir(t *testing.T) {
	got, err := nativeops.HomeDir()
	if err != nil {
		t.Fatalf("HomeDir: %v", err)
	}
	want, _ := os.UserHomeDir()
	if got != want {
		t.Errorf("HomeDir() = %q, want %q", got, want)
	}
}

func TestHostname(t *testing.T) {
	got, err := nativeops.Hostname()
	if err != nil {
		t.Fatalf("Hostname: %v", err)
	}
	if got == "" {
		t.Error("Hostname() = \"\", want a non-empty name")
	}
}

func TestPlatform(t *testing.T) {
	if got := nativeops.Platform(); got != "darwin" && got != "linux" && got != "windows" {
		t.Errorf("Platform() = %q, want one of darwin/linux/windows", got)
	}
}

func TestArch(t *testing.T) {
	if got := nativeops.Arch(); got == "" {
		t.Error("Arch() = \"\", want a non-empty GOARCH value")
	}
}

func TestCwd(t *testing.T) {
	got, err := nativeops.Cwd()
	if err != nil {
		t.Fatalf("Cwd: %v", err)
	}
	want, _ := os.Getwd()
	if got != want {
		t.Errorf("Cwd() = %q, want %q", got, want)
	}
}

func TestPid(t *testing.T) {
	if got := nativeops.Pid(); got != float64(os.Getpid()) {
		t.Errorf("Pid() = %v, want %v", got, os.Getpid())
	}
}
