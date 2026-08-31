package lint_test

import (
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

func TestStepTimeoutValidDurations(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step A timeout 3m { log("a") }
    parallel G timeout 10m {
        step B timeout 500ms { log("b") }
        step C { log("c") }
    }
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings for valid timeouts, got %+v", findings)
	}
}

func TestStepTimeoutNonPositiveFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    step A timeout 0s { log("a") }
    parallel G timeout 0m {
        step B { log("b") }
    }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `step "A": timeout "0s" is not a positive duration`) {
		t.Fatalf("expected a step-timeout finding, got %+v", findings)
	}
	if !hasMessage(findings, `parallel group "G": timeout "0m" is not a positive duration`) {
		t.Fatalf("expected a parallel-group-timeout finding, got %+v", findings)
	}
}
