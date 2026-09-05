package lint

import "github.com/mh-language/mhl-core-runtime/internal/lang/ast"

// checkExtensible statically validates every `extensible <kind> { ... }`
// declaration against the same structural rules
// internal/extension/external's loadExtensibleManifest enforces when it
// actually loads one — surfaced here with a file:line instead of requiring
// an `mhl extension install`/`doctor` round-trip to hit them. It does not
// duplicate the loader's semantic checks (a resolvable "manifest.id",
// api_version support, and so on) since those need the manifest object's
// values, not just its shape, and stay the loader's job alone.
func checkExtensible(file string, prog *ast.Program) []Finding {
	var findings []Finding
	for _, decl := range prog.Decls {
		if decl.Extensible == nil {
			continue
		}
		findings = append(findings, checkOneExtensible(file, decl.Extensible)...)
	}
	return findings
}

func checkOneExtensible(file string, ext *ast.Extensible) []Finding {
	var findings []Finding

	manifestCount := 0
	seenProps := map[string]bool{}
	seenMethods := map[string]bool{}
	for _, item := range ext.Items {
		switch {
		case item.Manifest != nil:
			manifestCount++
			switch {
			case manifestCount > 1:
				findings = append(findings, Finding{
					File: file, Line: item.Pos.Line, Column: item.Pos.Column,
					Message: `extensible ` + ext.Kind + `: "manifest" declared more than once`,
				})
			case ast.BareObject(item.Manifest) == nil:
				findings = append(findings, Finding{
					File: file, Line: item.Pos.Line, Column: item.Pos.Column,
					Message: `extensible ` + ext.Kind + `: "manifest" must be a literal object, e.g. { id: "...", api_version: "1", executable: "..." }`,
				})
			}
		case item.Properties != nil:
			for _, p := range item.Properties {
				if seenProps[p.Name] {
					findings = append(findings, Finding{
						File: file, Line: p.Pos.Line, Column: p.Pos.Column,
						Message: `extensible ` + ext.Kind + `: property "` + p.Name + `" declared more than once`,
					})
				}
				seenProps[p.Name] = true
			}
		case item.Method != nil:
			m := item.Method
			if seenMethods[m.Name] {
				findings = append(findings, Finding{
					File: file, Line: m.Pos.Line, Column: m.Pos.Column,
					Message: `extensible ` + ext.Kind + `: method "` + m.Name + `" declared more than once`,
				})
			}
			seenMethods[m.Name] = true
		}
	}
	if manifestCount == 0 {
		findings = append(findings, Finding{
			File: file, Line: ext.Pos.Line, Column: ext.Pos.Column,
			Message: `extensible ` + ext.Kind + `: missing a "manifest: { ... }" block`,
		})
	}
	return findings
}
