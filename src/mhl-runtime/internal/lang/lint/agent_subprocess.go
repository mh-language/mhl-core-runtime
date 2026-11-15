package lint

import (
	"fmt"
	"strings"

	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// checkAgentSubprocess validates the properties that shape an agent's
// subprocess beyond command/args — `cwd:`, `log:` and `log_mode:` — when
// they are written as literals, mirroring the interpreter (agent_process.go):
// cwd and log must be strings, log_mode must be "append" or "truncate" and
// means nothing without log, and cwd only applies to a subprocess (cli/*),
// never to an ollama/* HTTP call. An expression is only known at run time and
// is left to the interpreter.
func checkAgentSubprocess(file, name string, a *ast.Agent) []Finding {
	var findings []Finding
	add := func(p *ast.Property, msg string) {
		findings = append(findings, Finding{File: fileAt(file, p.Pos), Line: p.Pos.Line, Column: p.Pos.Column,
			Message: fmt.Sprintf("agent %q: %s", name, msg)})
	}
	engine, _ := ast.AgentEngine(a)
	var logProp, modeProp *ast.Property
	for _, p := range a.Props {
		switch p.Name {
		case "cwd":
			if strings.HasPrefix(engine, "ollama/") {
				add(p, "cwd applies only to a subprocess (cli/*) agent, not to "+engine)
			}
			if v, literal := ast.LiteralValue(p.Value); literal {
				if _, ok := v.(string); !ok {
					add(p, "cwd must be a string (a directory path)")
				}
			}
		case "log":
			logProp = p
			if v, literal := ast.LiteralValue(p.Value); literal {
				if _, ok := v.(string); !ok {
					add(p, "log must be a string (a file path)")
				}
			}
		case "log_mode":
			modeProp = p
		case "args":
			findings = append(findings, checkArgsElements(file, name, p)...)
		}
	}
	if modeProp != nil {
		if _, err := ast.AgentLogMode(a); err != nil {
			add(modeProp, `log_mode must be "append" or "truncate"`)
		} else if logProp == nil {
			add(modeProp, "log_mode has no effect without log")
		}
	}
	return findings
}

// checkArgsElements reports an `args:` array literal element the runtime
// would reject because it is a literal of some other type than string — a
// plain element (`42`) or an element of a spread literal (`...[1]`). A
// non-array spread operand is checkSpreadLiterals' finding.
func checkArgsElements(file, name string, p *ast.Property) []Finding {
	arr := ast.BareArray(p.Value)
	if arr == nil {
		return nil
	}
	var findings []Finding
	add := func(item *ast.Expr, msg string) {
		findings = append(findings, Finding{File: fileAt(file, item.Pos), Line: item.Pos.Line, Column: item.Pos.Column,
			Message: fmt.Sprintf("agent %q: %s", name, msg)})
	}
	for i, item := range arr.Items {
		if v, literal := spreadOperandLiteral(item); literal {
			if elems, ok := v.([]any); ok {
				for j, e := range elems {
					if _, isString := e.(string); !isString {
						add(item, fmt.Sprintf("args: element %d of a spread (...) must be a string, got %s", j, literalTypeName(e)))
					}
				}
			}
			continue
		}
		if v, literal := ast.LiteralValue(item); literal {
			if _, isString := v.(string); !isString {
				add(item, fmt.Sprintf("args[%d] must be a string, got %s", i, literalTypeName(v)))
			}
		}
	}
	return findings
}
