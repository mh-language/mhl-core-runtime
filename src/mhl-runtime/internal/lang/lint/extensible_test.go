package lint_test

import (
	"path/filepath"
	"testing"

	"github.com/mh-language/mhl-core-runtime/internal/lang/lint"
)

// A well-formed extensible declaration lints clean.
func TestExtensibleWellFormedIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "extension.mh")
	write(t, main, `
extensible cache {
    manifest: {
        id: "dev.mhl.cache-redis",
        api_version: "1",
        executable: "bin/mhl-cache-redis"
    }
    properties: {
        url: string
    }
    get(key: string) -> any
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// A leading `/** ... **/` doc block — the kind-level documentation header —
// must not trip up lint. This guards a real regression: parser.Parse
// originally only stripped that block inside
// internal/extension/external's own loader, so `mhl lint` (which calls
// parser.Parse directly) failed on any real extensible file using it.
func TestExtensibleWithDocBlockHeaderIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "extension.mh")
	write(t, main, `/**
TTL-first key/value cache backed by Redis.
**/
extensible cache {
    manifest: {
        id: "dev.mhl.cache-redis",
        api_version: "1",
        executable: "bin/mhl-cache-redis"
    }
    get(key: string) -> any /// The JSON-decoded value, or null.
}
`)
	if findings := lint.File(main); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestExtensibleMissingManifestIsALintError(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "extension.mh")
	write(t, main, `
extensible cache {
    get(key: string) -> any
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `missing a "manifest: { ... }" block`) {
		t.Fatalf("expected a missing-manifest finding, got %+v", findings)
	}
}

func TestExtensibleDuplicateManifestIsALintError(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "extension.mh")
	write(t, main, `
extensible cache {
    manifest: { id: "x", api_version: "1", executable: "bin/x" }
    manifest: { id: "y", api_version: "1", executable: "bin/y" }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `"manifest" declared more than once`) {
		t.Fatalf("expected a duplicate-manifest finding, got %+v", findings)
	}
}

func TestExtensibleNonObjectManifestIsALintError(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "extension.mh")
	write(t, main, `
extensible cache {
    manifest: "oops"
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `"manifest" must be a literal object`) {
		t.Fatalf("expected a non-object-manifest finding, got %+v", findings)
	}
}

func TestExtensibleDuplicatePropertyIsALintError(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "extension.mh")
	write(t, main, `
extensible cache {
    manifest: { id: "x", api_version: "1", executable: "bin/x" }
    properties: {
        url: string
        url: number
    }
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `property "url" declared more than once`) {
		t.Fatalf("expected a duplicate-property finding, got %+v", findings)
	}
}

func TestExtensibleDuplicateMethodIsALintError(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "extension.mh")
	write(t, main, `
extensible cache {
    manifest: { id: "x", api_version: "1", executable: "bin/x" }
    get(key: string) -> any
    get(key: string) -> string
}
`)
	findings := lint.File(main)
	if !hasMessage(findings, `method "get" declared more than once`) {
		t.Fatalf("expected a duplicate-method finding, got %+v", findings)
	}
}
