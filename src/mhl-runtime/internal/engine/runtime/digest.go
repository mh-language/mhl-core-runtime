package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strconv"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/mh-language/mhl-core-runtime/internal/lang/ast"
)

// Version is the runtime build version, stamped into every checkpoint so a
// --resume can report which build wrote the state it is continuing. internal/cli
// overwrites "dev" with the real version at startup; it is informational only —
// a mismatch does not block resume (only DefinitionDigest does).
var Version = "dev"

// StateSchemaVersion is the checkpoint on-disk format version. Bumped only when
// a change to the Checkpoint struct is not backward-readable; a resume of a
// checkpoint from a newer StateSchemaVersion than this build understands is
// refused.
const StateSchemaVersion = 1

// DefinitionDigest is a stable hash of the part of a resolved program that
// governs one pipeline's execution: the named pipeline/workflow's own
// declaration, plus every *shared* declaration it could reference — agents,
// tools, prompts, memory, extensions, type aliases, enums, imports (all
// merged into prog.Decls by ResolveImports). Source positions and comments
// are excluded, so a whitespace- or comment-only edit does not change it; a
// change to this pipeline's logic, or to a shared declaration, does.
//
// Deliberately NOT in the digest: other `pipeline`/`workflow` declarations
// and `test` blocks in the same file — editing a sibling pipeline or a test
// must not invalidate this pipeline's checkpoint.
//
// It is stamped into a checkpoint and checked on --resume: continuing a run
// against a definition whose digest no longer matches would splice old
// variable state into new control flow.
func DefinitionDigest(prog *ast.Program, pipeline string) string {
	h := sha256.New()
	for _, d := range prog.Decls {
		if d == nil {
			continue
		}
		if d.Test != nil {
			continue
		}
		if d.Pipeline != nil && d.Pipeline.Name != pipeline {
			continue
		}
		hashValue(h, reflect.ValueOf(d))
	}
	return hex.EncodeToString(h.Sum(nil))
}

var positionType = reflect.TypeOf(lexer.Position{})

// hashValue writes a deterministic, position-free encoding of v into h.
func hashValue(h interface{ Write([]byte) (int, error) }, v reflect.Value) {
	if !v.IsValid() {
		h.Write([]byte("nil;"))
		return
	}
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			h.Write([]byte("nil;"))
			return
		}
		hashValue(h, v.Elem())
	case reflect.Struct:
		if v.Type() == positionType {
			return // positions are not part of a program's meaning
		}
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue // unexported
			}
			h.Write([]byte(t.Field(i).Name))
			h.Write([]byte{':'})
			hashValue(h, v.Field(i))
			h.Write([]byte{';'})
		}
	case reflect.Slice, reflect.Array:
		h.Write([]byte("[" + strconv.Itoa(v.Len()) + "]"))
		for i := 0; i < v.Len(); i++ {
			hashValue(h, v.Index(i))
		}
	case reflect.Map:
		keys := v.MapKeys()
		sort.Slice(keys, func(a, b int) bool {
			return fmt.Sprint(keys[a].Interface()) < fmt.Sprint(keys[b].Interface())
		})
		for _, k := range keys {
			h.Write([]byte(fmt.Sprint(k.Interface())))
			h.Write([]byte{'='})
			hashValue(h, v.MapIndex(k))
		}
	case reflect.String:
		h.Write([]byte(strconv.Quote(v.String())))
	case reflect.Bool:
		if v.Bool() {
			h.Write([]byte{'T'})
		} else {
			h.Write([]byte{'F'})
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		h.Write([]byte(strconv.FormatInt(v.Int(), 10)))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		h.Write([]byte(strconv.FormatUint(v.Uint(), 10)))
	case reflect.Float32, reflect.Float64:
		h.Write([]byte(strconv.FormatFloat(v.Float(), 'g', -1, 64)))
	default:
		h.Write([]byte(v.String()))
	}
}
