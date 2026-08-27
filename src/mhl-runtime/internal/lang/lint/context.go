package lint

import (
	"fmt"
	"strings"

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
						File: file, Line: member.Prop.Pos.Line, Column: member.Prop.Pos.Column,
						Message: fmt.Sprintf("pipeline %q: context block has unknown key %q (known: require, source)", decl.Pipeline.Name, key),
					})
					continue
				}
				if key == "source" {
					if s, ok := ast.StringValue(f.Value); ok && !validContextSource(s) {
						findings = append(findings, Finding{
							File: file, Line: member.Prop.Pos.Line, Column: member.Prop.Pos.Column,
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

// pipelineHasContextProp reports whether p declares a `context:` block —
// which makes the identifier `context` a valid read inside every step of
// that pipeline (see checkAgentCalls's seed and interpreter.isContextRef).
func pipelineHasContextProp(p *ast.Pipeline) bool {
	for _, member := range p.Body {
		if member.Prop != nil && member.Prop.Name == "context" {
			return true
		}
	}
	return false
}
