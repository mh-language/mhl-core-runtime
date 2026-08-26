package lint_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanjustino/mhl-runtime/internal/lang/lint"
)

func TestCheckPipelineInputUnknownType(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    input count: sting
    step S {}
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `input "count": unrecognized type "sting"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
	if findings[0].Line != 3 {
		t.Errorf("expected line 3, got %d", findings[0].Line)
	}
}

func TestCheckPipelineInputKnownTypesClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    input issue_id: string
    input count: number
    input verbose: bool
    input strict_mode: boolean
    input vulnerabilities_found: int
    input tags: array
    input meta: object
    input anything: any
    step S {}
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckSkillFieldUnknownType mirrors TestCheckPipelineInputUnknownType
// for a skill's input/output field block.
func TestCheckSkillFieldUnknownType(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
skill K {
    input {
        target_file: sting
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `skill "K": field "target_file" has an unrecognized type "sting"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckSkillFieldBooleanIntAliasesClean is a regression guard: a real,
// CI-run fixture (internal/features/skills/resolver_test.go's projectSource)
// declares `strict_mode: boolean` and `vulnerabilities_found: int` — if the
// alias table in internal/lang/types didn't accept those spellings, this
// exact shape would become a brand-new lint failure the moment
// checkSkillFieldTypes shipped.
func TestCheckSkillFieldBooleanIntAliasesClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
export skill CodeAuditorSkill {
    description: "Analisa código."
    tools: [execution.read_file, execution.git_diff]
    mcp_servers: [PostgresDB]

    input {
        target_file: string
        strict_mode: boolean
    }

    output {
        vulnerabilities_found: int
        report_markdown: string
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckToolCallLiteralTypeMismatch confirms a literal argument of the
// wrong declared type is caught statically by checkToolCall.
func TestCheckToolCallLiteralTypeMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    read_file(path: string) -> fs.read(path)
}

pipeline P {
    step S {
        var content = execution.read_file(42)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	want := `tool "execution": read_file: parameter "path" must be string, got number`
	if !strings.Contains(findings[0].Message, want) {
		t.Errorf("unexpected message: %q, want to contain %q", findings[0].Message, want)
	}
}

// TestCheckToolCallVariableArgumentInferredTypeMismatch confirms a variable
// argument whose type is statically inferable (here, from its literal `var`
// initializer — see varinfer.go's mergeVarType) is now checked too, not just
// a literal argument — this is Fase 2b closing the gap
// TestCheckToolCallNonLiteralArgumentUnchecked used to lock in as
// unavoidable.
func TestCheckToolCallVariableArgumentInferredTypeMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    read_file(path: string) -> fs.read(path)
}

pipeline P {
    step S {
        var p = 42
        var content = execution.read_file(p)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	want := `tool "execution": read_file: parameter "path" must be string, got number`
	if !strings.Contains(findings[0].Message, want) {
		t.Errorf("unexpected message: %q, want to contain %q", findings[0].Message, want)
	}
}

// TestCheckToolCallVariableArgumentUninferableStillUnchecked keeps the
// "can't prove it, don't fail" guarantee alive: a variable whose value
// comes from something inference can't see through (an untyped-return call)
// must still be left unchecked, not wrongly flagged.
func TestCheckToolCallVariableArgumentUninferableStillUnchecked(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    read_file(path: string) -> fs.read(path)
    some_unknown() -> 42
}

pipeline P {
    step S {
        var p = execution.some_unknown()
        var content = execution.read_file(p)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for an uninferable argument, got %d: %+v", len(findings), findings)
	}
}

// TestCheckToolMethodReturnTypeUnrecognized confirms an unrecognized return
// type keyword is flagged, mirroring checkPipelineInputTypes' own typo check.
func TestCheckToolMethodReturnTypeUnrecognized(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    double(n: number): sting -> n * 2
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `tool "execution": double: unrecognized return type "sting"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckToolMethodReturnTypeExpressionBodyMismatch confirms a
// single-expression Body that's a literal of the wrong type is caught
// statically.
func TestCheckToolMethodReturnTypeExpressionBodyMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    bad(): number -> "not a number"
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	want := `tool "execution": bad: return value must be number, got string`
	if !strings.Contains(findings[0].Message, want) {
		t.Errorf("unexpected message: %q, want to contain %q", findings[0].Message, want)
	}
}

// TestCheckToolMethodReturnTypeBlockLiteralReturnMismatch confirms a literal
// `return <expr>` inside a Block (found via the nested if/while/try walk in
// toolMethodReturnExprs) is also caught statically.
func TestCheckToolMethodReturnTypeBlockLiteralReturnMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    classify(n: number): string -> {
        if (n > 0) {
            return 1
        }
        return "non-positive"
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	want := `tool "execution": classify: return value must be string, got number`
	if !strings.Contains(findings[0].Message, want) {
		t.Errorf("unexpected message: %q, want to contain %q", findings[0].Message, want)
	}
}

// TestCheckToolMethodReturnTypeNonLiteralUnchecked confirms a non-literal
// return (a native-op call) is left unchecked statically — the runtime
// check in evalToolCall is the net.
func TestCheckToolMethodReturnTypeNonLiteralUnchecked(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    read_file(path: string): string -> fs.read(path)
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for a non-literal return, got %d: %+v", len(findings), findings)
	}
}

// TestCheckToolMethodReturnTypeValidIsClean is a regression guard: a method
// with no Returns annotation at all must stay untouched by this check.
func TestCheckToolMethodReturnTypeUntypedIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    identity(v) -> v
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckToolCallPromptParamsNotSwept locks in that checkToolCall never
// walks ast.Prompt.Params — Param is shared by ToolMethod/Prompt/Lambda, and
// a real fixture (test/e2e/features/fixtures/prompts.mh) uses `role: prompt`
// as a pseudo-type meaning "must resolve to a declared prompt template", not
// one of the primitive kinds in internal/lang/types. If checkToolCall (or
// any future check) ever started sweeping Prompt.Params through
// types.Parse, this would start failing since "prompt" isn't a recognized
// type keyword.
func TestCheckToolCallPromptParamsNotSwept(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
export prompt SecurityExpertRole() {
    "Você é um especialista em segurança de software."
}

export prompt SystemPrompt(role: prompt, task: string) {
    """
    ${role}

    ${task}
    """
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// --- Fase 2b: variable/pipeline-input/mem type inference ------

// TestCheckToolCallChainedVariableInferredTypeMismatch confirms a var whose
// initializer is itself another already-known var (`var b = a`) chains the
// inferred type within the same forward pass.
func TestCheckToolCallChainedVariableInferredTypeMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    read_file(path: string) -> fs.read(path)
}

pipeline P {
    step S {
        var a = 42
        var b = a
        var content = execution.read_file(b)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `parameter "path" must be string, got number`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckToolCallTypedToolReturnInferredTypeMismatch confirms a var
// initialized from a call to a tool method with a declared return type
// (`): Type ->`) gets that type inferred, not just literals.
func TestCheckToolCallTypedToolReturnInferredTypeMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    double(n: number): number -> n * 2
    read_file(path: string) -> fs.read(path)
}

pipeline P {
    step S {
        var n = execution.double(21)
        var content = execution.read_file(n)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `parameter "path" must be string, got number`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckToolCallConflictingBranchesDowngradesToAny confirms a variable
// reassigned to a conflicting type inside an (unconditionally-visited,
// non-flow-sensitive) if-branch downgrades to Any rather than staying
// confidently wrong — no false positive, even though this specific program
// would in fact always take that branch and fail at runtime.
func TestCheckToolCallConflictingBranchesDowngradesToAny(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    read_file(path: string) -> fs.read(path)
}

pipeline P {
    step S {
        var p = 1
        if (true) {
            p = "a"
        }
        var content = execution.read_file(p)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings (downgraded to Any), got %d: %+v", len(findings), findings)
	}
}

// TestCheckToolCallPipelineInputArgumentMismatch confirms a pipeline
// `input name: Type` is seeded as a known type before a step's own
// statements run.
func TestCheckToolCallPipelineInputArgumentMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    read_file(path: string) -> fs.read(path)
}

pipeline P {
    input path: number
    step S {
        var content = execution.read_file(path)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `parameter "path" must be string, got number`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckToolCallPipelineInputMatchingTypeClean is the companion clean
// case: a correctly-typed pipeline input produces no finding.
func TestCheckToolCallPipelineInputMatchingTypeClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    read_file(path: string) -> fs.read(path)
}

pipeline P {
    input path: string
    step S {
        var content = execution.read_file(path)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckToolCallMemInitializerArgumentMismatch confirms a pipeline `mem`
// declaration is inferred the same way `var` is, from its initializer
// expression alone (mem's get-or-init runtime semantics don't matter to a
// purely static reader).
func TestCheckToolCallMemInitializerArgumentMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    read_file(path: string) -> fs.read(path)
}

loop pipeline P {
    mem cached_path = 42

    repeat: {
        stop_when: cached_path == 0
    }

    step S {
        var content = execution.read_file(cached_path)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `parameter "path" must be string, got number`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckToolCallIndexAssignDoesNotCorruptBaseVarType locks in the
// bareAssignName-vs-assignTargetBase distinction: assigning to arr[0] must
// never touch known["arr"]'s inferred type.
func TestCheckToolCallIndexAssignDoesNotCorruptBaseVarType(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    wants_array(items: array) -> items
}

pipeline P {
    step S {
        var arr = [1, 2, 3]
        arr[0] = "x"
        var result = execution.wants_array(arr)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings (arr must stay array-typed), got %d: %+v", len(findings), findings)
	}
}

// TestCheckToolCallSelfMethodReturnInferredInsideOwnBlock exercises the
// selfTool threading through inferExprType: a self.method(...) call inside
// a tool's own Block body should have its declared return type inferred
// too, same as calling another tool by name.
func TestCheckToolCallSelfMethodReturnInferredInsideOwnBlock(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    helper(): number -> 5
    read_file(path: string) -> fs.read(path)
    f() -> {
        var x = self.helper()
        return execution.read_file(x)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `parameter "path" must be string, got number`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckToolCallUntypedParamStillUncheckedWithVariableArgument locks in
// that an untyped param stays fully dynamic even when handed a variable
// argument whose type is in fact known.
func TestCheckToolCallUntypedParamStillUncheckedWithVariableArgument(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    identity(v) -> v
}

pipeline P {
    step S {
        var p = 42
        var result = execution.identity(p)
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// --- checkMemoryOp: key argument now also checked when inferable ------

// TestCheckMemoryOpKeyVariableWrongTypeStillFlagged is checkMemoryOp's
// counterpart to TestCheckToolCallVariableArgumentInferredTypeMismatch: a
// memory key passed as a variable whose type inference already knows is
// wrong (here, a number) is now flagged too, not just a literal key.
func TestCheckMemoryOpKeyVariableWrongTypeStillFlagged(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        var key = 42
        session_mem.set(key, "value")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `memory "session_mem": key must be a string`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckMemoryOpKeyVariableCorrectTypeIsClean is the companion clean
// case: a memory key variable correctly inferred as string produces no
// finding — a stricter regression guard than
// TestCheckMemoryOpWithVariableArgumentIsNotFlagged (which happens to use a
// string-literal key too, but wasn't written to specifically pin this down).
func TestCheckMemoryOpKeyVariableCorrectTypeIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}

pipeline P {
    step S {
        var key = "attempt"
        session_mem.set(key, "value")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckMemoryOpKeyVariableUninferableStillUnchecked keeps the "can't
// prove it, don't fail" guarantee alive for memory keys too.
func TestCheckMemoryOpKeyVariableUninferableStillUnchecked(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
memory session_mem {
    type: "kv"
    store: "memory"
}
tool execution {
    some_unknown() -> 42
}

pipeline P {
    step S {
        var key = execution.some_unknown()
        session_mem.set(key, "value")
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for an uninferable key, got %d: %+v", len(findings), findings)
	}
}

// --- richer structural types: array element type + object field shape ------

// TestCheckPipelineInputNestedArrayUnknownType confirms a typo inside an
// array-element type annotation (`sting[]`) is caught, quoting back the
// exact surface syntax the user wrote.
func TestCheckPipelineInputNestedArrayUnknownType(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
pipeline P {
    input tags: sting[]
    step S {}
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `input "tags": unrecognized type "sting[]"`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckSkillFieldNestedShapeUnknownType confirms a typo inside a nested
// object-shape field type (`{ age: sting }`) is caught too.
func TestCheckSkillFieldNestedShapeUnknownType(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
skill K {
    input {
        person: { name: string, age: sting }
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	if !strings.Contains(findings[0].Message, `field "person" has an unrecognized type`) {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

// TestCheckToolCallArrayElementTypeMismatch confirms a literal array
// argument with a wrong-typed element is flagged against a `string[]`
// parameter.
func TestCheckToolCallArrayElementTypeMismatch(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    upload(tags: string[]) -> tags
}

pipeline P {
    step S {
        var result = execution.upload(["a", 2])
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	want := `tool "execution": upload: parameter "tags"[1] must be string, got number`
	if !strings.Contains(findings[0].Message, want) {
		t.Errorf("unexpected message: %q, want to contain %q", findings[0].Message, want)
	}
}

// TestCheckToolCallArrayElementTypeMatchingIsClean is the companion clean
// case.
func TestCheckToolCallArrayElementTypeMatchingIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    upload(tags: string[]) -> tags
}

pipeline P {
    step S {
        var result = execution.upload(["a", "b"])
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

// TestCheckToolCallObjectShapeMissingField confirms a literal object
// argument missing a declared field is flagged against a `{ name: string,
// age: number }` parameter.
func TestCheckToolCallObjectShapeMissingField(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    register(person: { name: string, age: number }) -> person
}

pipeline P {
    step S {
        var result = execution.register({name: "Ana"})
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	want := `tool "execution": register: parameter "person": missing field "age" (must be number)`
	if !strings.Contains(findings[0].Message, want) {
		t.Errorf("unexpected message: %q, want to contain %q", findings[0].Message, want)
	}
}

// TestCheckToolCallObjectShapeExtraFieldIsClean locks in the permissive/
// structural subtyping decision: an extra, undeclared field on the literal
// argument must not be flagged.
func TestCheckToolCallObjectShapeExtraFieldIsClean(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.mh")
	write(t, main, `
tool execution {
    register(person: { name: string }) -> person
}

pipeline P {
    step S {
        var result = execution.register({name: "Ana", email: "a@b.com"})
    }
}
`)
	findings := lint.File(main)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings (extra field must be allowed), got %d: %+v", len(findings), findings)
	}
}
