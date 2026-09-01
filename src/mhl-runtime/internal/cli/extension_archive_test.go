package cli

import "testing"

func TestParseArchiveSource(t *testing.T) {
	cases := []struct {
		in       string
		ok       bool
		url, sha string
		isZip    bool
	}{
		{in: "https://h/x/mhl-store-s3_linux_amd64.tar.gz", ok: true,
			url: "https://h/x/mhl-store-s3_linux_amd64.tar.gz"},
		{in: "https://h/x/ext.tgz", ok: true, url: "https://h/x/ext.tgz"},
		{in: "http://h/x/ext.zip", ok: true, url: "http://h/x/ext.zip", isZip: true},
		{in: "https://h/x/ext.tar.gz#sha256=ABCdef0123", ok: true,
			url: "https://h/x/ext.tar.gz", sha: "abcdef0123"},
		{in: "https://h/x/ext.tar.gz#v1.2.0", ok: false}, // non-sha fragment
		{in: "https://h/x/repo.git", ok: false},          // git, not an archive
		{in: "https://h/x/ext.tar", ok: false},           // uncompressed tar not supported
		{in: "ftp://h/x/ext.tar.gz", ok: false},          // not http(s)
		{in: "./local/ext.tar.gz", ok: false},
		{in: "", ok: false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			as, ok := parseArchiveSource(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if !c.ok {
				return
			}
			if as.URL != c.url || as.SHA256 != c.sha || as.isZip != c.isZip || as.Raw != c.in {
				t.Fatalf("got %+v", as)
			}
		})
	}
}

func TestSafeJoin(t *testing.T) {
	base := "/tmp/x"
	for _, ok := range []string{"bin/mhl", "extension.json", "./a/b"} {
		if _, err := safeJoin(base, ok); err != nil {
			t.Errorf("safeJoin(%q) unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"../escape", "a/../../escape", "/abs/evil"} {
		if got, err := safeJoin(base, bad); err == nil {
			t.Errorf("safeJoin(%q) = %q, want escape error", bad, got)
		}
	}
}
