package ast

// Test declares a named suite of describe blocks, each grouping statements
// that exercise the pipelines/tools/skills declared elsewhere in the same
// program:
//
//	test CodeAuditPipelineTest {
//	    describe conditional_statements {
//	        are_equal("a", "a")
//	        if (x > 0) is_true(x > 0) else incomplete("negative case")
//	    }
//	}
//
// A describe block's Body reuses the exact same statement grammar a
// pipeline step's Body does (var/if/while/for-in/try/assign/expr) — there
// is no dedicated "assertion statement" grammar node. Recognizing a bare
// call like `are_equal(a, b)` as an assertion, versus an ordinary
// expression statement like `log(...)`, is internal/engine/interpreter's
// job at evaluation time (see assertionCall, test.go), not the parser's;
// this is what lets an assertion appear anywhere a statement can, including
// nested inside if/while/for-in.
type Test struct {
	Name      string      `parser:"'test' @Ident"`
	Describes []*Describe `parser:"'{' @@* '}'"`
}

// Describe is a named group of statements inside a test block.
type Describe struct {
	Name string       `parser:"'describe' @Ident"`
	Body []*Statement `parser:"'{' @@* '}'"`
}
