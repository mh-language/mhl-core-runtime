# sample

Worked examples of mhl (`.mh`) usage, one file per example. Every `.mh` file here is a
self-verifying `mhl test` program — run one directly with `mhl test <file>`, or a whole
directory with `mhl test <dir>`.

- [syntax/](syntax/README.md) — the expression and statement language itself (arithmetic,
  arrays, objects, strings, conditionals, loops), independent of any specific feature
- [features/](features/README.md) — the higher-level declarations a pipeline is built from
  (`agent`, `memory`, `prompt`) and how a `pipeline` wires them together

These examples were migrated from `src/mhl-runtime/test/e2e/{lang/syntax,features}`, one file
per original `describe` block, when that test suite was folded into this documentation-facing
sample tree.

> **Keeping this in sync:** any change to `src/mhl-runtime`'s grammar (new syntax, a new
> keyword, a new native op) must update this EBNF, and any behavioral change should come with
> a matching example under `syntax/` or `features/`.

## Language grammar (EBNF)

This is the authoritative surface grammar of `.mh`, transcribed directly from the Participle
v2 struct tags in `src/mhl-runtime/internal/lang/ast/*.go` (the grammar's real source of
truth — see that package's doc comment) and the token rules in
`src/mhl-runtime/internal/lang/parser/lexer.go`. Notation: `::=` defines a rule, `|`
alternation, `[ x ]` optional, `{ x }` zero-or-more, `( x )` grouping, `"x"` a literal token.

### Lexical grammar

```ebnf
letter      ::= "a".."z" | "A".."Z" ;
digit       ::= "0".."9" ;

Comment     ::= "//" { any character except newline } ;
Whitespace  ::= { " " | "\t" | "\r" | "\n" } ;         (* elided by the parser *)

Ident       ::= ( letter | "_" ) { letter | digit | "_" } ;
Number      ::= digit { digit } [ "." digit { digit } ] ;
Duration    ::= digit { digit } ( "ms" | "s" | "m" | "h" | "d" ) ;   (* tried before Number *)
String      ::= '"' { "\" any character | any character except '"' or "\" } '"' ;
MLString    ::= '"""' { any character } '"""' ;         (* tried before String *)
Punct       ::= ".." | "->" | "==" | "!=" | ">=" | "<=" | "&&" | "||"
              | "-" | "+" | "*" | "/" | "%" | "<" | ">" | "=" | "!" | "^"
              | "(" | ")" | "{" | "}" | "[" | "]" | ":" | "," | "." ;
```

Rule order matters: the lexer tries `MLString` before `String` (three quotes before one) and
`Duration` before `Number` (so `45s` lexes as one `Duration` token, not `45` then `s`), and
within `Punct`, `".."` is tried before the single-character class (so a slice's `..` lexes as
one token, not two `.` member-access tokens).

**String interpolation** is not a grammar production: `${...}` spans inside a `String` or
`MLString` token's text are found and re-parsed as a standalone `Expr` (via the same grammar,
rooted at `Expr` instead of `Program`) when the string is evaluated at runtime — see
`internal/lang/parser/parser.go`'s `ParseExpr`/`mhlExprParser`. A `\${...}` escape (backslash
immediately before `${`) is honored only for `prompt ... from "file.md"` bodies loaded from an
external Markdown file (`internal/features/prompt/render.go`), not in ordinary string
literals — see [features/prompts/prompt_loaded_from_markdown_file.mh](features/prompts/prompt_loaded_from_markdown_file.mh).

### Syntactic grammar

```ebnf
Program        ::= { Declaration } ;

Declaration    ::= [ "export" ]
                    ( Import | Use | Skill | Prompt | MCPServer
                    | Agent | Memory | Tool | Pipeline | Test ) ;

Import         ::= "import" String "as" Ident ;
Use            ::= "use" "{" UseItem { "," UseItem } "}" "from" String ;
UseItem        ::= Ident [ "as" Ident ] ;

MCPServer      ::= "mcp_server" Ident "{" { Property } "}" ;
Memory         ::= "memory" Ident "{" { Property } "}" ;
Agent          ::= "agent" [ Ident ] "{" { Property } "}" ;
                   (* Name is omitted for an inline agent literal, e.g. inside
                      a `fallback: [...]` list or anywhere an Expr is expected *)

Skill          ::= "skill" Ident "{" { SkillMember } "}" ;
SkillMember    ::= "input" FieldBlock | "output" FieldBlock | Property ;
FieldBlock     ::= "{" { Field } "}" ;
Field          ::= Ident ":" TypeExpr ;

Prompt         ::= "prompt" Ident "(" [ Param { "," Param } ] ")"
                    ( "{" Expr "}" | "from" String ) ;
                   (* inline body, or the body loaded from an external .md file *)

Tool           ::= "tool" Ident "{" { ToolMethod } "}" ;
ToolMethod     ::= Ident "(" [ Param { "," Param } ] ")" [ ":" TypeExpr ]
                    "->" ( Expr | "{" { Statement } "}" ) ;
                   (* a single-expression body is tried before a block body *)

Param          ::= Ident [ ":" TypeExpr ] ;
Property       ::= Ident ":" Expr ;

TypeExpr       ::= ( Ident | ObjectShape ) { "[" "]" } ;
                   (* each trailing "[]" is one level of array nesting *)
ObjectShape    ::= "{" [ ShapeField { [ "," ] ShapeField } [ "," ] ] "}" ;
ShapeField     ::= Ident ":" TypeExpr ;

Pipeline       ::= [ "loop" ] "pipeline" Ident "{" { PipelineMember } "}" ;
PipelineMember ::= "input" PipelineInput | VarDecl | MemDecl | Step | Property ;
PipelineInput  ::= Ident ":" TypeExpr ;
MemDecl        ::= "mem" Ident "=" Expr ;
                   (* pipeline-scoped, persistent, get-or-init across
                      `loop pipeline` iterations and --resume *)
Step           ::= "step" Ident "{" { Statement } "}" ;

Test           ::= "test" Ident "{" { Describe } "}" ;
Describe       ::= "describe" Ident "{" { Statement } "}" ;
                   (* a Describe body is ordinary Statement grammar --
                      there is no separate "assertion" production; a call
                      like are_equal(a, b) is recognized as an assertion by
                      the interpreter, not the parser *)

Statement      ::= VarDecl | ReturnStmt | BreakStmt | GotoStmt | IfStmt
                  | WhileStmt | ForInStmt | TryStmt | AssignStmt | ExprStmt ;

VarDecl        ::= "var" Ident "=" Expr ;
ReturnStmt     ::= "return" [ Expr ] ;
BreakStmt      ::= "break" [ Expr ] ;
GotoStmt       ::= "goto" Ident ;
IfStmt         ::= "if" "(" Expr ")" Block [ "else" Block ] ;
WhileStmt      ::= "while" "(" Expr ")" Block ;
ForInStmt      ::= "for" "(" "var" Ident "in" Expr ")" Block ;
TryStmt        ::= "try" "{" { Statement } "}"
                    "catch" [ "(" Ident ")" ] "{" { Statement } "}"
                    [ "finally" "{" { Statement } "}" ] ;
AssignStmt     ::= Postfix "=" Expr ;
ExprStmt       ::= Expr ;

Block          ::= "{" { Statement } "}" | Statement ;
                   (* every control-flow body accepts a braced block or one
                      bare inline statement, e.g. `if (cond) log("yes")` *)

Expr           ::= OrExpr ;
OrExpr         ::= AndExpr { "||" AndExpr } ;
AndExpr        ::= EqExpr { "&&" EqExpr } ;
EqExpr         ::= CmpExpr { ( "==" | "!=" ) CmpExpr } ;
CmpExpr        ::= AddExpr { ( "<=" | ">=" | "<" | ">" ) AddExpr } ;
AddExpr        ::= MulExpr { ( "+" | "-" ) MulExpr } ;
MulExpr        ::= Unary { ( "*" | "/" | "%" ) Unary } ;
Unary          ::= [ "!" | "-" ] Postfix ;
Postfix        ::= Primary { Trailer } ;
Trailer        ::= "." Ident | Call | "[" Slice "]" | "[" Expr "]" ;
                   (* Slice is tried before a plain index -- both start with
                      "[" Expr, but Slice requires a ".." that a plain index
                      never has *)
Slice          ::= [ SliceBound ] ".." [ SliceBound ] ;
SliceBound     ::= [ "^" ] Expr ;
                   (* a leading "^" counts the bound from the end: numbers[^3..] *)
Call           ::= "(" [ Argument { "," Argument } ] ")" ;
Argument       ::= [ Ident ":" ] Expr ;

Primary        ::= Duration | String | MLString | Number
                  | ( "true" | "false" ) | "null"
                  | Object | Array | Agent | Lambda | IfExpr
                  | Ident | "(" Expr ")" ;
                   (* Lambda is tried before the parenthesized-Expr form --
                      both start with "(", disambiguated by Lambda's
                      mandatory trailing "->" *)

IfExpr         ::= "if" "(" Expr ")" Expr "else" Expr ;
                   (* the value-producing ternary form; unlike IfStmt, both
                      branches are a bare Expr and "else" is mandatory *)
Lambda         ::= "(" [ Param { "," Param } ] ")" "->" ( Expr | "{" { Statement } "}" ) ;
Object         ::= "{" [ ObjectField { [ "," ] ObjectField } [ "," ] ] "}" ;
ObjectField    ::= ( String | Ident ) ":" Expr ;
Array          ::= "[" [ Expr { "," Expr } [ "," ] ] "]" ;
```

### Notes

- **Precedence** (lowest to highest binding): `||` → `&&` → `== !=` → `< > <= >=` →
  `+ -` → `* / %` → unary `! -` → postfix (`.member`, `(call)`, `[index]`, `[slice]`).
  There is no exponent operator.
- **Native op namespaces** (`cmd`, `git`, `fs`, `http`, `json`, `log`) are not separate
  grammar — `cmd.exec(...)` parses as ordinary `Postfix` (`Ident` `.member` `Call`). The
  reserved namespace/method pairs live in `internal/engine/interpreter/tool.go`'s
  `nativeOpCall` (e.g. `cmd.exec`, `cmd.exec_all`, `git.diff`, `git.add`, `git.commit`,
  `git.status`, `git.rev_parse`, `git.log`, `fs.read`, `fs.exists`, `fs.write`, `fs.append`,
  `fs.delete`, `fs.join`, `fs.list`, `http.post`, `json.parse`, `json.parse_lines`,
  `json.stringify`, `log.info`, `log.warn`, `log.error`), not the grammar.
- **`env(...)`** and assertion calls (`are_equal`, `is_true`, `is_false`, `is_null`,
  `not_null`, `incomplete`, ...) are ordinary `Ident` `Call` expressions too — recognized by
  name at evaluation time, not by any dedicated grammar rule.
- Per CLAUDE.md, some fields *documented* in `docs/language-design.md` /
  `docs/language-specification.md` parse under this grammar (they're valid `Property`/`Expr`
  values) but aren't read by the interpreter yet — this EBNF describes what **parses**, not
  what every property does at runtime.
