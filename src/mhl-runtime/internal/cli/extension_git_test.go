package cli

import "testing"

func TestParseGitSource(t *testing.T) {
	cases := []struct {
		in                    string
		ok                    bool
		url, subdir, ref, raw string
	}{
		{in: "https://github.com/acme/mhl-crm.git", ok: true,
			url: "https://github.com/acme/mhl-crm.git", raw: "https://github.com/acme/mhl-crm.git"},
		{in: "https://github.com/acme/mhl-crm.git#v1.2.0", ok: true,
			url: "https://github.com/acme/mhl-crm.git", ref: "v1.2.0"},
		{in: "https://github.com/acme/mono.git//ext/crm#main", ok: true,
			url: "https://github.com/acme/mono.git", subdir: "ext/crm", ref: "main"},
		{in: "https://github.com/acme/mono.git//src/mhl-extensions/mhl-store-s3", ok: true,
			url: "https://github.com/acme/mono.git", subdir: "src/mhl-extensions/mhl-store-s3"},
		{in: "git@github.com:acme/mhl-crm.git", ok: true, url: "git@github.com:acme/mhl-crm.git"},
		{in: "git@github.com:acme/mono.git//ext#release/1.0", ok: true,
			url: "git@github.com:acme/mono.git", subdir: "ext", ref: "release/1.0"},
		{in: "ssh://git@example.com/acme/crm", ok: true, url: "ssh://git@example.com/acme/crm"},
		{in: "git+https://example.com/acme/crm.git", ok: true, url: "https://example.com/acme/crm.git"},
		{in: "./local/ext", ok: false},
		{in: "/abs/path/ext", ok: false},
		{in: "ext", ok: false},
		{in: "", ok: false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			gs, ok := parseGitSource(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if !c.ok {
				return
			}
			if gs.URL != c.url {
				t.Errorf("URL = %q, want %q", gs.URL, c.url)
			}
			if gs.Subdir != c.subdir {
				t.Errorf("Subdir = %q, want %q", gs.Subdir, c.subdir)
			}
			if gs.Ref != c.ref {
				t.Errorf("Ref = %q, want %q", gs.Ref, c.ref)
			}
			if gs.Raw != c.in {
				t.Errorf("Raw = %q, want %q", gs.Raw, c.in)
			}
		})
	}
}
