package lint

import (
	"reflect"

	"github.com/alecthomas/participle/v2/lexer"
)

// fileAt is the file a finding at pos belongs to: the module the position
// was parsed from (stampFile), or file — the entry file being linted — for a
// position of the entry file itself. Every check runs on the entry file's
// program merged with its imports, so a declaration it reports on may live
// in an imported module; labelling that finding with the entry file would
// pair the module's line number with the wrong file.
func fileAt(file string, pos lexer.Position) string {
	if pos.Filename != "" {
		return pos.Filename
	}
	return file
}

var positionType = reflect.TypeOf(lexer.Position{})

// stampFile sets Filename on every lexer.Position in node (an imported
// module's freshly parsed AST), so fileAt can tell its declarations apart
// from the entry file's once both are merged into one program.
func stampFile(node any, file string) {
	stampValue(reflect.ValueOf(node), file)
}

func stampValue(v reflect.Value, file string) {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if !v.IsNil() {
			stampValue(v.Elem(), file)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			stampValue(v.Index(i), file)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			stampValue(v.MapIndex(k), file)
		}
	case reflect.Struct:
		if v.Type() == positionType {
			if v.CanSet() {
				v.FieldByName("Filename").SetString(file)
			}
			return
		}
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath == "" {
				stampValue(v.Field(i), file)
			}
		}
	}
}
