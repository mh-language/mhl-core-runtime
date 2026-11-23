package interpreter

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"sync"

	"github.com/mh-language/mhl-core-runtime/internal/features/prompt"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// Prompt variants (docs/MHL-PROMPT-VARIANTS.md). A prompt call still
// evaluates to a plain string — its default text — so it flows through
// vars, tool parameters typed `string`, returns, object fields, `==` and
// every string operation exactly as before. What lets an agent later pick a
// `for` arm (`prompt.target(t)`) is a per-run side table: each rendering of
// a prompt that has variants (or whose arguments do) is recorded under its
// default text, with the declaration and arguments that produced it.
// `text.target(t)` looks text up there and re-renders the arm t selects;
// a string that is not a recorded rendering — a literal, or a prompt text
// transformed by concatenation, a tool, `json`, a file round trip — is its
// own target and comes back unchanged.
//
// The table lives for one run (WithPromptRegistry, set by execsvc.Run), so a
// rendering made in one step is still known in the next. It is not part of
// the checkpoint: a prompt rendered before a pause or a crash resolves to
// its default text after the resume.

// promptRendering is one recorded rendering: decl's default text rendered
// with args. nested holds, per argument, the recorded renderings that
// argument's text matched, so a nested prompt follows the same target.
type promptRendering struct {
	decl   *ast.Prompt
	args   map[string]string
	nested map[string][]*promptRendering
}

func (r *promptRendering) sameAs(o *promptRendering) bool {
	if r.decl != o.decl || !maps.Equal(r.args, o.args) || len(r.nested) != len(o.nested) {
		return false
	}
	for k, a := range r.nested {
		b, ok := o.nested[k]
		if !ok || len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
	}
	return true
}

// renderFor renders the arm target selects, with each nested argument
// resolved for the same target. variant is the arm used (nil = default).
func (r *promptRendering) renderFor(target string) (string, *ast.PromptVariant, error) {
	args := r.args
	if len(r.nested) > 0 {
		args = maps.Clone(r.args)
		for name, cands := range r.nested {
			text, _, err := resolveCandidates(r.args[name], cands, target)
			if err != nil {
				return "", nil, err
			}
			args[name] = text
		}
	}
	v := r.decl.Variant(target)
	text, err := prompt.RenderBody(r.decl, v, args)
	return text, v, err
}

// resolveCandidates renders text for target from every rendering recorded
// under it. Two different renderings can share a default text only when the
// default ignores an argument some arm uses; if they then disagree for this
// target there is no way to know which one was meant, and that is an error
// rather than a silent pick.
func resolveCandidates(text string, cands []*promptRendering, target string) (string, *promptRendering, error) {
	var out string
	var picked *promptRendering
	for i, c := range cands {
		t, _, err := c.renderFor(target)
		if err != nil {
			return "", nil, err
		}
		if i == 0 {
			out, picked = t, c
			continue
		}
		if t != out {
			return "", nil, fmt.Errorf("prompt %q: two renderings share the same default text but differ for target %q — "+
				"make the default arm use every parameter its variants use", c.decl.Name, target)
		}
	}
	if picked == nil {
		return text, nil, nil
	}
	return out, picked, nil
}

// promptRegistry is the per-run table of recorded renderings, keyed by
// default text. Concurrent `parallel` branches and `spawn`ed agents share it.
type promptRegistry struct {
	mu     sync.Mutex
	byText map[string][]*promptRendering
}

func newPromptRegistry() *promptRegistry {
	return &promptRegistry{byText: map[string][]*promptRendering{}}
}

func (reg *promptRegistry) record(text string, r *promptRendering) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for _, existing := range reg.byText[text] {
		if existing.sameAs(r) {
			return
		}
	}
	reg.byText[text] = append(reg.byText[text], r)
}

func (reg *promptRegistry) lookup(text string) []*promptRendering {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return append([]*promptRendering(nil), reg.byText[text]...)
}

type promptRegistryKey struct{}

// WithPromptRegistry returns ctx carrying a fresh prompt-rendering table for
// one run, so `prompt.target(t)` recognizes a prompt rendered in an earlier
// step of the same run. execsvc.Run calls it once per run; a context without
// one (a test, a direct RunStep) gets a table per evaluation root instead.
func WithPromptRegistry(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, promptRegistryKey{}, newPromptRegistry())
}

// rootPrompts is the table an evaluation root starts with: the run's (from
// goctx) when there is one, else a fresh one. Setting it at the root — not
// lazily — keeps every derived context (withBindings and friends copy the
// evalCtx shallowly) on the same table.
func rootPrompts(goctx context.Context) *promptRegistry {
	if goctx != nil {
		if reg, ok := goctx.Value(promptRegistryKey{}).(*promptRegistry); ok {
			return reg
		}
	}
	return newPromptRegistry()
}

// promptsOf returns this evaluation's table: the one ctx already holds, else
// the run's (from goctx), else a new one bound to ctx.
func promptsOf(ctx *evalCtx) *promptRegistry {
	if ctx == nil {
		return newPromptRegistry()
	}
	if ctx.prompts == nil {
		if reg, ok := goctxOf(ctx).Value(promptRegistryKey{}).(*promptRegistry); ok {
			ctx.prompts = reg
		} else {
			ctx.prompts = newPromptRegistry()
		}
	}
	return ctx.prompts
}

// recordPromptRendering records text (decl rendered with args) when it can
// matter to a later `.target`: decl declares variants, or an argument was
// itself a recorded rendering. A prompt without variants anywhere in it
// leaves the table untouched.
func recordPromptRendering(ctx *evalCtx, decl *ast.Prompt, args map[string]string, text string) {
	reg := promptsOf(ctx)
	var nested map[string][]*promptRendering
	for name, v := range args {
		if cands := reg.lookup(v); len(cands) > 0 {
			if nested == nil {
				nested = map[string][]*promptRendering{}
			}
			nested[name] = cands
		}
	}
	if len(decl.Variants) == 0 && len(nested) == 0 {
		return
	}
	reg.record(text, &promptRendering{decl: decl, args: args, nested: nested})
}

// promptTargetText is `text.target(target)`: the arm target selects when
// text is a recorded prompt rendering, else text itself. When ctx collects
// picks (an agent with `trace: true`), the selection is noted there.
func promptTargetText(ctx *evalCtx, text, target string) (string, error) {
	cands := promptsOf(ctx).lookup(text)
	if len(cands) == 0 {
		return text, nil
	}
	out, picked, err := resolveCandidates(text, cands, target)
	if err != nil {
		return "", err
	}
	if ctx != nil && ctx.promptPicks != nil && picked != nil {
		arm := "default"
		if v := picked.decl.Variant(target); v != nil {
			arm = fmt.Sprintf("for %q", v.Pattern)
		}
		*ctx.promptPicks = append(*ctx.promptPicks, fmt.Sprintf("prompt %s → %s (target %q)", picked.decl.Name, arm, target))
	}
	return out, nil
}

// callPromptTarget implements the `.target(t)` string method.
func callPromptTarget(ctx *evalCtx, receiver string, args []any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("target() requires exactly one argument (the harness/model target)")
	}
	t, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("target() argument must be a string, got %s", typeName(args[0]))
	}
	return promptTargetText(ctx, receiver, t)
}

// promptTargetSpan matches the literal `${prompt.target(X)}` placeholder an
// agent's `args` element or `stdin:` may use, where X is one of the agent's
// declared inputs or a string literal.
const promptTargetPrefix = "${prompt.target("

// parsePromptTargetSpan reads the argument of one `${prompt.target(X)}`
// span starting at s[0]; it returns the span's length, the argument as
// written, and whether X is a quoted string literal.
func parsePromptTargetSpan(s string) (n int, arg string, quoted bool, ok bool) {
	if !strings.HasPrefix(s, promptTargetPrefix) {
		return 0, "", false, false
	}
	rest := s[len(promptTargetPrefix):]
	end := strings.Index(rest, ")}")
	if end < 0 {
		return 0, "", false, false
	}
	arg = strings.TrimSpace(rest[:end])
	if len(arg) >= 2 && arg[0] == '"' && arg[len(arg)-1] == '"' {
		return len(promptTargetPrefix) + end + 2, arg[1 : len(arg)-1], true, true
	}
	if !isIdent(arg) {
		return 0, "", false, false
	}
	return len(promptTargetPrefix) + end + 2, arg, false, true
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// promptTargetArg resolves X of a `${prompt.target(X)}` span: a quoted
// literal is itself; an identifier must name one of agent's inputs (the
// same rule substituteInputPlaceholders follows for `${input}`).
func promptTargetArg(ctx *evalCtx, agent *ast.Agent, arg string, quoted bool) (string, error) {
	if quoted {
		return arg, nil
	}
	for _, in := range agent.Inputs {
		if in.Name == arg && ctx != nil {
			return formatValue(ctx.env[arg]), nil
		}
	}
	return "", fmt.Errorf("${prompt.target(%s)}: %q is not a declared input of agent %q (use an input or a string literal, or the expression form prompt.target(...))", arg, arg, agent.Name)
}

// substitutePromptSpans fills every `${prompt}`, `${schema}` and
// `${prompt.target(X)}` span of template in one pass, so text substituted in
// is never rescanned for placeholders. targeted reports whether a
// `${prompt.target(...)}` span was present.
func substitutePromptSpans(ctx *evalCtx, agent *ast.Agent, template, promptText, schemaText string) (out string, targeted bool, err error) {
	var b strings.Builder
	for i := 0; i < len(template); {
		rest := template[i:]
		switch {
		case strings.HasPrefix(rest, "${prompt}"):
			b.WriteString(promptText)
			i += len("${prompt}")
		case strings.HasPrefix(rest, "${schema}"):
			if schemaText == "" {
				return "", false, fmt.Errorf(`stdin declares "${schema}" but no schema: argument was supplied`)
			}
			b.WriteString(schemaText)
			i += len("${schema}")
		default:
			if n, arg, quoted, ok := parsePromptTargetSpan(rest); ok {
				t, err := promptTargetArg(ctx, agent, arg, quoted)
				if err != nil {
					return "", false, err
				}
				text, err := promptTargetText(ctx, promptText, t)
				if err != nil {
					return "", false, err
				}
				b.WriteString(text)
				targeted = true
				i += n
				continue
			}
			b.WriteByte(template[i])
			i++
		}
	}
	return b.String(), targeted, nil
}

// exprReadsPrompt reports whether expr (an agent property's expression
// form) reads the name `prompt` anywhere — a `stdin:`/`args` that composes
// the prompt itself (e.g. `stdin: prompt.target(model)`), which is then the
// prompt's only destination: it is not also appended to argv.
func exprReadsPrompt(expr *ast.Expr) bool {
	return readsPrompt(reflect.ValueOf(expr))
}

// readsPrompt walks v by reflection (like parser.checkSpreadPlacement) for a
// Primary that names `prompt` or a string literal that interpolates it.
func readsPrompt(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return false
		}
		if p, ok := v.Interface().(*ast.Primary); ok {
			if p.Ident == "prompt" ||
				(p.Str != nil && strings.Contains(*p.Str, "${prompt")) ||
				(p.MultiStr != nil && strings.Contains(*p.MultiStr, "${prompt")) {
				return true
			}
		}
		return readsPrompt(v.Elem())
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if readsPrompt(v.Index(i)) {
				return true
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() && readsPrompt(v.Field(i)) {
				return true
			}
		}
	}
	return false
}

// promptPlacement records where an agent's declaration sends the prompt,
// which decides whether injectPromptArg still appends it to argv.
type promptPlacement struct {
	stdinLiteral     bool // stdin: is a bare string literal (a template)
	stdinTemplated   bool // ... that has a ${prompt} or ${prompt.target(X)} span
	stdinTargeted    bool // ... with a ${prompt.target(X)} span
	stdinReadsPrompt bool // stdin: is an expression that reads `prompt`
	argsReadsPrompt  bool // an args expression (not a literal element) reads `prompt`
	argsTargeted     bool // an args literal element was ${prompt.target(X)}
}

// placedElsewhere reports whether the prompt already reaches the process some
// other way than an args `${prompt}` element, so it must not be appended.
func (p promptPlacement) placedElsewhere() bool {
	return p.stdinTemplated || p.stdinReadsPrompt || p.argsReadsPrompt || p.argsTargeted
}

func agentPromptPlacement(agent *ast.Agent) promptPlacement {
	var pl promptPlacement
	for _, prop := range agent.Props {
		switch prop.Name {
		case "stdin":
			if s, ok := ast.StringValue(prop.Value); ok {
				pl.stdinLiteral = true
				pl.stdinTemplated = strings.Contains(s, "${prompt}") || strings.Contains(s, promptTargetPrefix)
			} else {
				pl.stdinReadsPrompt = exprReadsPrompt(prop.Value)
			}
		case "args":
			if arr := ast.BareArray(prop.Value); arr != nil {
				for _, item := range arr.Items {
					if _, literal := ast.StringValue(item); !literal && exprReadsPrompt(item) {
						pl.argsReadsPrompt = true
					}
				}
			} else if exprReadsPrompt(prop.Value) {
				pl.argsReadsPrompt = true
			}
		}
	}
	return pl
}

// resolveArgsPromptTargets replaces every args element that is exactly a
// `${prompt.target(X)}` placeholder with the variant X selects.
func resolveArgsPromptTargets(ctx *evalCtx, agent *ast.Agent, args []string, promptText string, pl *promptPlacement) ([]string, error) {
	var out []string
	for i, a := range args {
		n, arg, quoted, ok := parsePromptTargetSpan(a)
		if !ok || n != len(a) {
			continue
		}
		t, err := promptTargetArg(ctx, agent, arg, quoted)
		if err != nil {
			return nil, err
		}
		text, err := promptTargetText(ctx, promptText, t)
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = append([]string{}, args...)
		}
		out[i] = text
		pl.argsTargeted = true
	}
	if out == nil {
		return args, nil
	}
	return out, nil
}

// resolveAgentStdin turns the agent's resolved stdin value into the text the
// process reads: a literal template has its `${prompt}`, `${schema}` and
// `${prompt.target(X)}` spans filled in one pass; an expression that reads
// `prompt` already composed the prompt itself and is used as-is; any other
// expression keeps the old ${prompt}/${schema} substitution.
func resolveAgentStdin(ctx *evalCtx, agent *ast.Agent, stdinTemplate, promptText, schemaText string, pl *promptPlacement) (string, error) {
	switch {
	case stdinTemplate == "":
		return "", nil
	case pl.stdinLiteral:
		text, targeted, err := substitutePromptSpans(ctx, agent, stdinTemplate, promptText, schemaText)
		pl.stdinTargeted = targeted
		return text, err
	case pl.stdinReadsPrompt:
		return stdinTemplate, nil
	default:
		return resolveStdinText(stdinTemplate, promptText, schemaText)
	}
}
