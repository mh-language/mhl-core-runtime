package lsp

import (
	"strings"
	"testing"
)

func hasLabel(items []completionItem, label string) bool {
	for _, it := range items {
		if it.Label == label {
			return true
		}
	}
	return false
}

// posAtMarker locates the first "§" in text and returns the source with the
// marker stripped out, plus the position it marked — lets a test place the
// cursor inline in a source literal instead of counting lines/columns by
// hand.
func posAtMarker(t *testing.T, text string) (string, position) {
	t.Helper()
	idx := strings.Index(text, "§")
	if idx < 0 {
		t.Fatalf("posAtMarker: no § marker in text: %s", text)
	}
	before := text[:idx]
	line := strings.Count(before, "\n")
	col := len(before) - strings.LastIndex(before, "\n") - 1
	return before + text[idx+len("§"):], position{Line: line, Character: col}
}

func TestCompletionNativeNamespaceMembers(t *testing.T) {
	cases := []struct {
		namespace string
		want      []string
	}{
		{"log", []string{"info", "warn", "error"}},
		{"cmd", []string{"exec", "exec_all"}},
		{"git", []string{"diff", "add", "commit", "status", "rev_parse", "log"}},
		{"fs", []string{"read", "exists", "write", "append", "delete", "list", "join"}},
		{"http", []string{"get", "post", "put", "patch", "delete", "head", "options", "download"}},
		{"json", []string{"parse", "parse_lines", "stringify"}},
		{"time", []string{"now", "parse", "format", "add", "diff", "compare"}},
	}
	for _, c := range cases {
		text := c.namespace + "."
		items := completionAt("main.mh", text, position{Line: 0, Character: len(text)})
		for _, want := range c.want {
			if !hasLabel(items, want) {
				t.Errorf("%s.: missing member %q, got %+v", c.namespace, want, items)
			}
		}
	}
}

func TestCompletionOffersNativeNamespaceNames(t *testing.T) {
	items := completionAt("main.mh", "", position{Line: 0, Character: 0})
	for _, name := range []string{"cmd", "git", "fs", "http", "json", "log", "time"} {
		if !hasLabel(items, name) {
			t.Errorf("general completion missing native namespace %q", name)
		}
	}
}

func TestCompletionLocalVarMembers(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
		skip []string
	}{
		{
			name: "string var offers string methods, mid-typing the trailer (broken parse)",
			src: `
pipeline P {
    step S {
        var content = "a\r\nb"
        content.§
    }
}
`,
			want: []string{"replace", "split", "trim", "starts_with", "ends_with", "to_upper", "to_lower", "substring", "contains", "size", "is_empty"},
			skip: []string{"keys", "values", "filter", "find", "sort_by", "get_index"},
		},
		{
			name: "array var offers array methods",
			src: `
pipeline P {
    step S {
        var items = [1, 2, 3]
        items.§
    }
}
`,
			want: []string{"filter", "find", "sort_by", "get_index", "index_of", "size", "is_empty", "contains"},
			skip: []string{"replace", "split", "trim", "keys", "values"},
		},
		{
			name: "object var offers object methods",
			src: `
pipeline P {
    step S {
        var config = {a: 1}
        config.§
    }
}
`,
			want: []string{"keys", "values", "size", "is_empty"},
			skip: []string{"replace", "filter", "get_index"},
		},
		{
			name: "agent.run() result is inferred as string",
			src: `
agent Claude {
    command: "claude"
}
pipeline P {
    step S {
        var response = Claude.run(prompt: "hi")
        response.§
    }
}
`,
			want: []string{"replace", "split", "trim"},
		},
		{
			name: "var from an unresolvable expression offers nothing",
			src: `
pipeline P {
    step S {
        var total = a + b
        total.§
    }
}
`,
			skip: []string{"replace", "filter", "keys", "size"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, pos := posAtMarker(t, c.src)
			items := completionAt("main.mh", text, pos)
			for _, want := range c.want {
				if !hasLabel(items, want) {
					t.Errorf("missing %q, got %+v", want, items)
				}
			}
			for _, skip := range c.skip {
				if hasLabel(items, skip) {
					t.Errorf("unexpectedly offered %q", skip)
				}
			}
		})
	}
}

// TestCompletionExtensionMembers proves an extension declaration's
// dot-callable methods come from the registered adapter's metadata.
func TestCompletionExtensionMembers(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "extension mcp",
			src:  "extension mcp GitHub {\n url: \"x\"\n}\npipeline P {\n step S {\n  GitHub.§\n }\n}\n",
			want: []string{"call", "list_tools", "discover"},
		},
		{
			name: "extension a2a",
			src:  "extension a2a Remote {\n url: \"x\"\n}\npipeline P {\n step S {\n  Remote.§\n }\n}\n",
			want: []string{"send", "agent_card", "get_task", "cancel"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, pos := posAtMarker(t, c.src)
			items := completionAt("main.mh", text, pos)
			for _, want := range c.want {
				if !hasLabel(items, want) {
					t.Errorf("missing %q, got %+v", want, items)
				}
			}
		})
	}
}

func TestCompletionPropertyPosition(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
		skip []string // must NOT be offered — proves the context classifier actually narrowed things
	}{
		{
			name: "plain pipeline body offers checkpoint and context but not repeat",
			src: `
pipeline P {
    §
}
`,
			want: []string{"checkpoint", "context"},
			skip: []string{"repeat"},
		},
		{
			name: "inside context object",
			src: `
pipeline P {
    context: {
        §
    }
}
`,
			want: []string{"source", "require"},
			skip: []string{"checkpoint", "enabled"},
		},
		{
			name: "loop pipeline body offers checkpoint and repeat",
			src: `
loop pipeline P {
    §
}
`,
			want: []string{"checkpoint", "repeat"},
		},
		{
			name: "inside checkpoint object",
			src: `
pipeline P {
    checkpoint: {
        §
    }
}
`,
			want: []string{"enabled", "strategy", "storage", "ttl"},
		},
		{
			name: "inside repeat object",
			src: `
loop pipeline P {
    repeat: {
        §
    }
}
`,
			want: []string{"stop_when", "max_iterations"},
		},
		{
			name: "agent body",
			src: `
agent X {
    §
}
`,
			want: []string{"engine", "command", "args", "endpoint", "temperature", "log", "trace", "retry", "cache", "rate_limit", "fallback", "before", "after"},
		},
		{
			name: "extension a2a body",
			src: `
extension a2a Translator {
    §
}
`,
			want: []string{"url", "headers", "poll_interval", "poll_timeout"},
			skip: []string{"engine", "transport"},
		},
		{
			name: "extension mcp body",
			src: `
extension mcp GitHub {
    §
}
`,
			want: []string{"transport", "command", "args", "url", "headers", "protocol"},
			skip: []string{"engine", "poll_interval"},
		},
		{
			name: "inside agent retry object",
			src: `
agent X {
    retry: {
        §
    }
}
`,
			want: []string{"max_attempts", "delay", "retry_on", "backoff"},
		},
		{
			name: "inside agent cache object",
			src: `
agent X {
    cache: {
        §
    }
}
`,
			want: []string{"ttl", "storage", "strategy"},
		},
		{
			name: "inside agent rate_limit object",
			src: `
agent X {
    rate_limit: {
        §
    }
}
`,
			want: []string{"requests_per_minute", "concurrency", "on_exceeded"},
		},
		{
			name: "inline fallback agent literal is also agent context",
			src: `
agent X {
    fallback: [
        agent {
            §
        }
    ]
}
`,
			want: []string{"engine", "retry"},
		},
		{
			name: "inside a step body offers no property-position noise",
			src: `
pipeline P {
    step S {
        §
    }
}
`,
			skip: []string{"checkpoint", "repeat", "enabled", "stop_when", "engine"},
		},
		{
			name: "plain pipeline body offers the parallel keyword",
			src: `
pipeline P {
    §
}
`,
			want: []string{"parallel", "step"},
		},
		{
			name: "inside a parallel group body offers step, not property noise",
			src: `
pipeline P {
    parallel G {
        §
    }
}
`,
			want: []string{"step"},
			skip: []string{"checkpoint", "enabled", "max_concurrency", "engine"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, pos := posAtMarker(t, c.src)
			items := completionAt("main.mh", text, pos)
			for _, want := range c.want {
				if !hasLabel(items, want) {
					t.Errorf("missing %q, got %+v", want, items)
				}
			}
			for _, skip := range c.skip {
				if hasLabel(items, skip) {
					t.Errorf("unexpectedly offered %q", skip)
				}
			}
		})
	}
}

func TestCompletionTypeAnnotationPosition(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
		skip []string // must NOT be offered — proves this doesn't false-positive on ordinary properties
	}{
		{
			name: "pipeline input type position",
			src: `
pipeline P {
    input count: §
    step S {}
}
`,
			want: []string{"string", "number", "bool", "array", "object", "any"},
		},
		{
			name: "tool method param type position",
			src: `
tool execution {
    read_file(path: §
}
`,
			want: []string{"string", "number", "bool", "array", "object", "any"},
		},
		{
			name: "ordinary agent property is not offered type keywords",
			src: `
agent X {
    command: §
}
`,
			skip: []string{"string", "number", "bool", "array", "object", "any"},
		},
		{
			name: "ordinary agent body property position keeps its own keywords",
			src: `
agent X {
    §
}
`,
			want: []string{"engine", "command"},
			skip: []string{"string", "number", "bool", "array", "object", "any"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, pos := posAtMarker(t, c.src)
			items := completionAt("main.mh", text, pos)
			for _, want := range c.want {
				if !hasLabel(items, want) {
					t.Errorf("missing %q, got %+v", want, items)
				}
			}
			for _, skip := range c.skip {
				if hasLabel(items, skip) {
					t.Errorf("unexpectedly offered %q", skip)
				}
			}
		})
	}
}
