package mhfile

import "testing"

func TestIs(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"main.mh", true},
		{"main.mhl", true},
		{"dir/sub/flow.partial.mh", true},
		{"dir/sub/flow.partial.mhl", true},
		{"main.MH", false},
		{"main.mhx", false},
		{"main.mh.bak", false},
		{"prompt.prompt.md", false},
		{"schema.json", false},
		{"mh", false},
	}
	for _, c := range cases {
		if got := Is(c.name); got != c.want {
			t.Errorf("Is(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
