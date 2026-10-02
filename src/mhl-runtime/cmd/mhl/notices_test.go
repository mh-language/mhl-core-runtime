package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// TestThirdPartyNoticesCoverLinkedModules fails when a module linked into
// the mhl binary has no entry in the repository's THIRD_PARTY_NOTICES.md —
// the file every release archive ships so the dependencies' licenses (MIT,
// BSD-3-Clause) are reproduced alongside the binary. Adding a dependency to
// go.mod means adding its license text there too.
func TestThirdPartyNoticesCoverLinkedModules(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Skip("no build info in this test binary")
	}
	notices, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "THIRD_PARTY_NOTICES.md"))
	if err != nil {
		t.Fatalf("reading THIRD_PARTY_NOTICES.md: %v", err)
	}
	text := string(notices)
	for _, dep := range info.Deps {
		if !strings.Contains(text, dep.Path) {
			t.Errorf("module %s (%s) is linked into mhl but has no entry in THIRD_PARTY_NOTICES.md", dep.Path, dep.Version)
		}
	}
}
