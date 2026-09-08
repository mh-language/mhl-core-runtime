package lsp

import (
	"runtime"
	"testing"
)

// TestUriToPathDriveLetter guards against a regression of the Windows
// drive-letter URI bug: a client sends "file:///c%3A/Users/..." (VS Code
// and the LSP spec both do), which net/url decodes to a Path of
// "/c:/Users/..." — a leading slash ahead of the drive letter that isn't a
// valid Windows path, breaking every filepath.*/os.* call downstream
// (`prompt X() from "..."` and `import` resolution, go-to-definition).
func TestUriToPathDriveLetter(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"file:///c%3A/Users/dev/project/main.mh", "c:/Users/dev/project/main.mh"},
		{"file:///C:/Users/dev/project/main.mh", "C:/Users/dev/project/main.mh"},
		{"file:///home/dev/project/main.mh", "/home/dev/project/main.mh"},
	}
	for _, tt := range tests {
		if got := uriToPath(tt.uri); got != tt.want {
			t.Errorf("uriToPath(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}

// TestPathToURIDriveLetter is uriToPath's inverse: a drive-letter path must
// round-trip to a "file:///C:/..." URI, not one where url.URL mistakes the
// drive letter for a host. Uses forward slashes so the leading-slash-
// injection logic under test doesn't depend on filepath.ToSlash's
// GOOS-specific separator conversion (exercised separately below).
func TestPathToURIDriveLetter(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"C:/Users/dev/project/main.mh", "file:///C:/Users/dev/project/main.mh"},
		{"/home/dev/project/main.mh", "file:///home/dev/project/main.mh"},
	}
	for _, tt := range tests {
		if got := pathToURI(tt.path); got != tt.want {
			t.Errorf("pathToURI(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// TestPathToURIWindowsSeparators only exercises anything under
// GOOS=windows, where filepath.ToSlash actually converts "\" to "/" — the
// backslash-separated paths filepath.Dir/Join produce there.
func TestPathToURIWindowsSeparators(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("filepath.ToSlash only converts \\ on windows")
	}
	if got, want := pathToURI(`C:\Users\dev\project\main.mh`), "file:///C:/Users/dev/project/main.mh"; got != want {
		t.Errorf("pathToURI = %q, want %q", got, want)
	}
}

// TestURIPathRoundTripWindows exercises uriToPath then pathToURI together,
// the way definitionAt does when resolving an import/prompt `from "..."`
// target and handing the location back to the client.
func TestURIPathRoundTripWindows(t *testing.T) {
	uri := "file:///c%3A/Users/dev/project/main.mh"
	path := uriToPath(uri)
	if got, want := pathToURI(path), "file:///c:/Users/dev/project/main.mh"; got != want {
		t.Errorf("round trip = %q, want %q", got, want)
	}
}
