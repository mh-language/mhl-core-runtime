package lint

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// knownContextKeys are the fields a pipeline's `context: { ... }` block
// accepts. Kept in sync by hand with runtime.contextConfigFromExpr
// (internal/engine/runtime/pipeline.go): internal/lang may not import
// internal/engine, so this is the lint-side twin, the same arrangement
// repeatStopWhenExpr (loop.go) has with runtime.repeatConfigFromExpr.
var knownContextKeys = map[string]bool{"source": true, "require": true}

// checkPipelineContext flags an unrecognized key inside a pipeline's
// `context: { ... }` block. The runtime reader silently ignores an unknown
// key, so a typo there would just leave the pipeline behaving as if the
// field were never written.
func checkPipelineContext(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Pipeline == nil {
			continue
		}
		for _, member := range decl.Pipeline.Body {
			if member.Prop == nil || member.Prop.Name != "context" {
				continue
			}
			obj := ast.BareObject(member.Prop.Value)
			if obj == nil {
				continue
			}
			for _, f := range obj.Fields {
				key := ""
				switch {
				case f.KeyIdent != nil:
					key = *f.KeyIdent
				case f.KeyStr != nil:
					key = *f.KeyStr
				}
				if !knownContextKeys[key] {
					findings = append(findings, Finding{
						File: fileAt(file, member.Prop.Pos), Line: member.Prop.Pos.Line, Column: member.Prop.Pos.Column,
						Message: fmt.Sprintf("pipeline %q: context block has unknown key %q (known: require, source)", decl.Pipeline.Name, key),
					})
					continue
				}
				if key == "source" {
					if s, ok := ast.StringValue(f.Value); ok && !validContextSource(s) {
						findings = append(findings, Finding{
							File: fileAt(file, member.Prop.Pos), Line: member.Prop.Pos.Line, Column: member.Prop.Pos.Column,
							Message: fmt.Sprintf("pipeline %q: context source %q is not recognized — use \"latest\" or \"session:<id>\"", decl.Pipeline.Name, s),
						})
					}
				}
			}
		}
	}
	return findings
}

// validContextSource mirrors runtime.PriorVars's dispatch on
// ContextConfig.Source: the only forms it acts on are the literal "latest"
// and "session:<non-empty-id>". Anything else is silently treated as
// "latest" at run time, so lint flags it as a likely typo.
func validContextSource(s string) bool {
	if s == "latest" {
		return true
	}
	return strings.HasPrefix(s, "session:") && strings.TrimSpace(s[len("session:"):]) != ""
}

// checkContextVarsDeprecated warns on the deprecated way of reading a prior
// run's state: the `context: { source, require }` block, and every
// `context.vars` read (steps, hooks, `output:`, `mem` initializers). A run
// does not read another run's state; `mem` keeps what one session needs
// (across loop iterations and --resume). The run metadata
// (`context.session_id`, `.started_at`, `.resumed`, `.principal`) needs no
// block and is not deprecated. Both still work as before.
func checkContextVarsDeprecated(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		p := decl.Pipeline
		if p == nil {
			continue
		}
		for _, m := range p.Body {
			if m.Prop != nil && m.Prop.Name == "context" {
				findings = append(findings, Finding{File: fileAt(file, m.Prop.Pos), Line: m.Prop.Pos.Line, Column: m.Prop.Pos.Column, Severity: SeverityWarning,
					Message: fmt.Sprintf("%s %q: `context: { ... }` is deprecated — a run does not read another run's state; keep what this run needs in a `mem` variable (`mem build = 0`, kept across loop iterations and --resume); context.session_id, .started_at, .resumed and .principal need no block", pipelineKind(p), p.Name)})
			}
		}
		walkPostfixesIn(reflect.ValueOf(p), p.Pos, func(pf *ast.Postfix, pos lexer.Position) {
			if pf.Primary == nil || pf.Primary.Ident != "context" || len(pf.Ops) == 0 || pf.Ops[0].Member != "vars" {
				return
			}
			findings = append(findings, Finding{File: fileAt(file, pos), Line: pos.Line, Column: pos.Column, Severity: SeverityWarning,
				Message: "`context.vars` is deprecated — a run does not read another run's state; keep what this run needs in a `mem` variable (kept across loop iterations and --resume)"})
		})
	}
	return findings
}
