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
		findings = append(findings, Finding{File: file, Line: p.Pos.Line, Column: p.Pos.Column,
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
