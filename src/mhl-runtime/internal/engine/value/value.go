// Package value holds mhl's runtime value model beyond the plain JSON shapes
// (string, float64, bool, nil, []any, map[string]any): a reference object,
// and the copy/compare/encode operations that give the language its two
// kinds of structure.
//
//   - A plain object/array (`{...}`, `[...]`) is a *value*: every read of a
//     variable holding one yields an independent copy (DeepCopy), so two
//     names never share a mutable structure — `b = a; b["x"] = 1` leaves a
//     unchanged, in a direct run and after a checkpoint/resume alike.
//   - A reference object (`ref { ... }`, *Ref) is shared: copying a
//     structure that contains one copies the pointer, so every name that
//     holds it sees every mutation. Each Ref carries an ID that is
//     persisted with the checkpoint (EncodeRefs/DecodeRefs), so after a
//     resume, occurrences with the same ID are the same object again.
//
// A Ref never crosses the interpreter's boundary to the outside world (JSON
// output, native ops, memory, MCP results): Materialize turns it into a
// plain object there.
package value

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Ref is a reference object: a mutable field map with identity.
type Ref struct {
	ID     string
	Fields map[string]any
}

// NewRef wraps fields as a new reference object with a fresh ID.
func NewRef(fields map[string]any) *Ref {
	if fields == nil {
		fields = map[string]any{}
	}
	return &Ref{ID: newID(), Fields: fields}
}

// ObjectFields lets internal/lang/types treat a Ref as an object without
// importing this package (types.ObjectCarrier).
func (r *Ref) ObjectFields() map[string]any { return r.Fields }

// MarshalJSON renders a Ref as its plain fields (Materialize) — so every
// json.Marshal-based boundary (json.stringify, log, `${...}`, a run's JSON
// result) shows an object, never the id. A ref cycle is an error.
func (r *Ref) MarshalJSON() ([]byte, error) {
	m, err := Materialize(r)
	if err != nil {
		return nil, err
	}
	return json.Marshal(m)
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("value: reading random ref id: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// DeepCopy copies the plain structure of v — every []any and
// map[string]any, recursively — and shares every *Ref as-is: a value is
// copied, a reference is not. Scalars are immutable and returned unchanged.
func DeepCopy(v any) any {
	switch t := v.(type) {
	case []any:
		c := make([]any, len(t))
		for i := range t {
			c[i] = DeepCopy(t[i])
		}
		return c
	case map[string]any:
		c := make(map[string]any, len(t))
		for k := range t {
			c[k] = DeepCopy(t[k])
		}
		return c
	default:
		return v
	}
}

// CloneRefs is DeepCopy that also clones every *Ref (keeping its ID), for
// handing a structure to a concurrently running `parallel` branch: the
// branch may then mutate its refs without racing its siblings, and
// MergeRefs reconciles the clones by ID at the barrier. clones maps each
// original Ref to its clone, so a Ref reachable twice (or cyclically) is
// cloned once and identity holds within the branch.
func CloneRefs(v any, clones map[*Ref]*Ref) any {
	switch t := v.(type) {
	case []any:
		c := make([]any, len(t))
		for i := range t {
			c[i] = CloneRefs(t[i], clones)
		}
		return c
	case map[string]any:
		c := make(map[string]any, len(t))
		for k := range t {
			c[k] = CloneRefs(t[k], clones)
		}
		return c
	case *Ref:
		if c, ok := clones[t]; ok {
			return c
		}
		c := &Ref{ID: t.ID, Fields: map[string]any{}}
		clones[t] = c
		for k, fv := range t.Fields {
			c.Fields[k] = CloneRefs(fv, clones)
		}
		return c
	default:
		return v
	}
}

// Equal compares two values structurally, except that two refs are equal
// only when they are the same object (same ID) — the identity a reference
// stands for.
func Equal(a, b any) bool {
	switch x := a.(type) {
	case *Ref:
		y, ok := b.(*Ref)
		return ok && x.ID == y.ID
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, xv := range x {
			yv, ok := y[k]
			if !ok || !Equal(xv, yv) {
				return false
			}
		}
		return true
	default:
		if _, isRef := b.(*Ref); isRef {
			return false
		}
		switch b.(type) {
		case []any, map[string]any:
			return false
		}
		return a == b
	}
}

// Materialize returns v with every *Ref replaced by a plain copy of its
// fields — the form a value takes once it leaves the interpreter (JSON,
// native ops, memory, a run's result). A ref cycle has no plain form and is
// an error.
func Materialize(v any) (any, error) {
	return materialize(v, map[*Ref]bool{})
}

// MaterializeLossy is Materialize for display-only state (a paused run's
// partial vars): the back edge of a ref cycle becomes the string
// "[circular ref]" instead of an error. The checkpoint, not this view, is
// what preserves the cycle.
func MaterializeLossy(v any) any {
	m, _ := materializeWith(v, map[*Ref]bool{}, true)
	return m
}

func materialize(v any, onPath map[*Ref]bool) (any, error) {
	return materializeWith(v, onPath, false)
}

func materializeWith(v any, onPath map[*Ref]bool, lossy bool) (any, error) {
	switch t := v.(type) {
	case []any:
		c := make([]any, len(t))
		for i := range t {
			mv, err := materializeWith(t[i], onPath, lossy)
			if err != nil {
				return nil, err
			}
			c[i] = mv
		}
		return c, nil
	case map[string]any:
		c := make(map[string]any, len(t))
		for k := range t {
			mv, err := materializeWith(t[k], onPath, lossy)
			if err != nil {
				return nil, err
			}
			c[k] = mv
		}
		return c, nil
	case *Ref:
		if onPath[t] {
			if lossy {
				return "[circular ref]", nil
			}
			return nil, fmt.Errorf("a ref object that contains itself can't be converted to plain data (JSON)")
		}
		onPath[t] = true
		defer delete(onPath, t)
		return materializeWith(t.Fields, onPath, lossy)
	default:
		return v, nil
	}
}

// MaterializeVars is Materialize over a variable map.
func MaterializeVars(vars map[string]any) (map[string]any, error) {
	if vars == nil {
		return nil, nil
	}
	out := make(map[string]any, len(vars))
	for k, v := range vars {
		mv, err := Materialize(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		out[k] = mv
	}
	return out, nil
}

// HasRefs reports whether v contains a *Ref anywhere.
func HasRefs(v any) bool {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			if HasRefs(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range t {
			if HasRefs(e) {
				return true
			}
		}
	case *Ref:
		return true
	}
	return false
}

// Checkpoint encoding. A Ref is written as {"$mhl_ref": id, "fields": {...}}
// the first time it is met and as {"$mhl_ref": id} after that (which also
// makes a cycle representable); DecodeRefs rebuilds one object per id. A
// plain object that happens to use the reserved key is escaped as
// {"$mhl_plain": {...}}.
const (
	refKey   = "$mhl_ref"
	plainKey = "$mhl_plain"
)

// EncodeRefs returns vars with every Ref replaced by its checkpoint marker.
// Identity is shared across the whole map: the same Ref under two
// variables is written in full once.
func EncodeRefs(vars map[string]any) map[string]any {
	if vars == nil {
		return nil
	}
	seen := map[*Ref]bool{}
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names) // deterministic: which occurrence carries the fields
	out := make(map[string]any, len(vars))
	for _, k := range names {
		out[k] = encode(vars[k], seen)
	}
	return out
}

func encode(v any, seen map[*Ref]bool) any {
	switch t := v.(type) {
	case []any:
		c := make([]any, len(t))
		for i := range t {
			c[i] = encode(t[i], seen)
		}
		return c
	case map[string]any:
		c := make(map[string]any, len(t))
		for _, k := range sortedKeys(t) {
			c[k] = encode(t[k], seen)
		}
		if _, reserved := t[refKey]; reserved {
			return map[string]any{plainKey: c}
		}
		if _, reserved := t[plainKey]; reserved {
			return map[string]any{plainKey: c}
		}
		return c
	case *Ref:
		if seen[t] {
			return map[string]any{refKey: t.ID}
		}
		seen[t] = true
		fields := make(map[string]any, len(t.Fields))
		for _, k := range sortedKeys(t.Fields) {
			fields[k] = encode(t.Fields[k], seen)
		}
		return map[string]any{refKey: t.ID, "fields": fields}
	default:
		return v
	}
}

// DecodeRefs reverses EncodeRefs: every marker with the same id becomes the
// same *Ref. A map with no markers comes back unchanged (a checkpoint
// written before refs existed decodes as-is).
func DecodeRefs(vars map[string]any) map[string]any {
	if vars == nil {
		return nil
	}
	refs := map[string]*Ref{}
	// Pass 1: create every ref that carries its fields, so a bare id marker
	// met before its full occurrence (map order) still resolves.
	for _, v := range vars {
		collect(v, refs)
	}
	out := make(map[string]any, len(vars))
	for k, v := range vars {
		out[k] = decode(v, refs)
	}
	return out
}

func collect(v any, refs map[string]*Ref) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			collect(e, refs)
		}
	case map[string]any:
		if id, ok := t[refKey].(string); ok {
			if fields, ok := t["fields"].(map[string]any); ok {
				if _, exists := refs[id]; !exists {
					refs[id] = &Ref{ID: id, Fields: map[string]any{}}
				}
				for _, e := range fields {
					collect(e, refs)
				}
			}
			return
		}
		for _, e := range t {
			collect(e, refs)
		}
	}
}

func decode(v any, refs map[string]*Ref) any {
	switch t := v.(type) {
	case []any:
		c := make([]any, len(t))
		for i := range t {
			c[i] = decode(t[i], refs)
		}
		return c
	case map[string]any:
		if inner, ok := t[plainKey].(map[string]any); ok && len(t) == 1 {
			c := make(map[string]any, len(inner))
			for k, e := range inner {
				c[k] = decode(e, refs)
			}
			return c
		}
		if id, ok := t[refKey].(string); ok {
			r := refs[id]
			if r == nil { // a bare marker whose full occurrence is missing
				r = &Ref{ID: id, Fields: map[string]any{}}
				refs[id] = r
			}
			if fields, ok := t["fields"].(map[string]any); ok && len(r.Fields) == 0 {
				for k, e := range fields {
					r.Fields[k] = decode(e, refs)
				}
			}
			return r
		}
		c := make(map[string]any, len(t))
		for k, e := range t {
			c[k] = decode(e, refs)
		}
		return c
	default:
		return v
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CollectRefs records every *Ref reachable from v into refs, keyed by ID
// (the first object met for an ID wins). Cycle-safe.
func CollectRefs(v any, refs map[string]*Ref) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			CollectRefs(e, refs)
		}
	case map[string]any:
		for _, e := range t {
			CollectRefs(e, refs)
		}
	case *Ref:
		if _, seen := refs[t.ID]; seen {
			return
		}
		refs[t.ID] = t
		for _, e := range t.Fields {
			CollectRefs(e, refs)
		}
	}
}

// Canonicalize returns v with every *Ref replaced by canon[ID] (when
// present), so that after merging copies there is exactly one object per
// ID. It also canonicalizes the fields of every canonical ref it reaches,
// once each (cycle-safe via done).
func Canonicalize(v any, canon map[string]*Ref, done map[*Ref]bool) any {
	switch t := v.(type) {
	case []any:
		for i := range t {
			t[i] = Canonicalize(t[i], canon, done)
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = Canonicalize(t[k], canon, done)
		}
		return t
	case *Ref:
		c := t
		if cr, ok := canon[t.ID]; ok {
			c = cr
		}
		if !done[c] {
			done[c] = true
			for k := range c.Fields {
				c.Fields[k] = Canonicalize(c.Fields[k], canon, done)
			}
		}
		return c
	default:
		return v
	}
}
