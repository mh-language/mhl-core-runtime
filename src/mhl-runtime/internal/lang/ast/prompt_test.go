package ast

import "testing"

func TestPromptVariantPrecedence(t *testing.T) {
	p := &Prompt{Variants: []*PromptVariant{
		{Pattern: "claude/*"},
		{Pattern: "claude/opus*"},
		{Pattern: "claude/opus-5"},
		{Pattern: "cli/codex"},
		{Pattern: "ollama*"},
	}}
	cases := map[string]string{
		"":                "",
		"claude/sonnet":   "claude/*",
		"claude/opus-4":   "claude/opus*",
		"claude/opus-5":   "claude/opus-5",
		"claude/opus-5.1": "claude/opus*",
		"cli/codex":       "cli/codex",
		"cli/codex2":      "",
		"ollama/qwen":     "ollama*",
		"claude":          "",
	}
	for target, want := range cases {
		got := ""
		if v := p.Variant(target); v != nil {
			got = v.Pattern
		}
		if got != want {
			t.Errorf("Variant(%q) = %q, want %q", target, got, want)
		}
	}
}
