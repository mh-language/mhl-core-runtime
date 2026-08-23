// Package interpreter is the tree-walking evaluator for a parsed .mh
// program: it executes pipeline step statements (exec.go), evaluates
// expressions (eval.go), and dispatches the calls a step body can make —
// agent.run(...) (agent.go), memory.method(...) (memory_ops.go), a
// `prompt:` argument (prompt_ops.go), a declared `tool` method (tool.go),
// and the reserved cmd/git/fs/http native-op namespaces (also tool.go) —
// against the language features under internal/features. imports.go
// resolves `import`/`use` declarations before a program is run.
//
// Scope: execution only. Parsing a .mh file into an AST is
// internal/lang/parser's job; this package only ever consumes an
// *ast.Program already produced by it. The command-line surface
// (internal/cli) is this package's only caller.
package interpreter
