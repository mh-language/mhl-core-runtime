package value

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDeepCopyCopiesValuesAndSharesRefs(t *testing.T) {
	r := NewRef(map[string]any{"n": 1.0})
	orig := map[string]any{"plain": map[string]any{"x": 1.0}, "ref": r}
	c := DeepCopy(orig).(map[string]any)

	c["plain"].(map[string]any)["x"] = 2.0
	if orig["plain"].(map[string]any)["x"] != 1.0 {
		t.Fatal("a plain object must be copied")
	}
	c["ref"].(*Ref).Fields["n"] = 2.0
	if r.Fields["n"] != 2.0 {
		t.Fatal("a ref must be shared, not copied")
	}
}

func TestCloneRefsKeepsIDAndIdentityWithinTheClone(t *testing.T) {
	r := NewRef(map[string]any{"n": 1.0})
	orig := map[string]any{"a": r, "b": []any{r}}
	clones := map[*Ref]*Ref{}
	c := CloneRefs(orig, clones).(map[string]any)

	ca, cb := c["a"].(*Ref), c["b"].([]any)[0].(*Ref)
	if ca == r || ca.ID != r.ID {
		t.Fatal("a clone is a distinct object with the same id")
	}
	if ca != cb {
		t.Fatal("the same ref reached twice clones to one object")
	}
	ca.Fields["n"] = 9.0
	if r.Fields["n"] != 1.0 {
		t.Fatal("mutating the clone must not touch the original")
	}
}

func TestEqualComparesRefsByIdentity(t *testing.T) {
	a := NewRef(map[string]any{"x": 1.0})
	b := NewRef(map[string]any{"x": 1.0})
	if Equal(a, b) {
		t.Fatal("two refs with equal fields are still different objects")
	}
	if !Equal(a, a) || !Equal(map[string]any{"r": a}, map[string]any{"r": a}) {
		t.Fatal("the same ref is equal to itself, nested too")
	}
	if !Equal(map[string]any{"x": 1.0}, map[string]any{"x": 1.0}) || Equal(map[string]any{"x": 1.0}, a) {
		t.Fatal("plain values compare structurally, and never equal a ref")
	}
}

func TestMaterialize(t *testing.T) {
	r := NewRef(map[string]any{"n": 1.0, "inner": NewRef(map[string]any{"k": "v"})})
	m, err := Materialize(map[string]any{"r": r})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	if string(b) != `{"r":{"inner":{"k":"v"},"n":1}}` {
		t.Fatalf("materialized = %s", b)
	}

	cyc := NewRef(map[string]any{})
	cyc.Fields["self"] = cyc
	if _, err := Materialize(cyc); err == nil || !strings.Contains(err.Error(), "contains itself") {
		t.Fatalf("a cycle has no plain form, got %v", err)
	}
}

// roundTrip mirrors the checkpoint path: encode, JSON, decode.
func roundTrip(t *testing.T, vars map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(EncodeRefs(vars))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	return DecodeRefs(back)
}

func TestEncodeDecodePreservesIdentity(t *testing.T) {
	r := NewRef(map[string]any{"n": 1.0})
	got := roundTrip(t, map[string]any{"a": r, "b": map[string]any{"deep": r}, "plain": map[string]any{"x": 1.0}})

	a, b := got["a"].(*Ref), got["b"].(map[string]any)["deep"].(*Ref)
	if a != b || a.ID != r.ID || a.Fields["n"] != 1.0 {
		t.Fatalf("same id must decode to the same object: a=%#v b=%#v", a, b)
	}
	a.Fields["n"] = 2.0
	if b.Fields["n"] != 2.0 {
		t.Fatal("after decode the two names share the object")
	}
	if _, ok := got["plain"].(map[string]any); !ok {
		t.Fatal("a plain object decodes as a plain object")
	}
}

func TestEncodeDecodeCycle(t *testing.T) {
	r := NewRef(map[string]any{"name": "loop"})
	r.Fields["self"] = r
	got := roundTrip(t, map[string]any{"r": r})
	back := got["r"].(*Ref)
	if back.Fields["self"].(*Ref) != back {
		t.Fatal("a cycle must decode back to a cycle")
	}
}

func TestEncodeEscapesReservedKey(t *testing.T) {
	vars := map[string]any{"o": map[string]any{"$mhl_ref": "not-a-ref", "x": 1.0}}
	got := roundTrip(t, vars)
	o, ok := got["o"].(map[string]any)
	if !ok || o["$mhl_ref"] != "not-a-ref" || o["x"] != 1.0 {
		t.Fatalf("a plain object using the reserved key must survive as plain: %#v", got["o"])
	}
}

func TestDecodeLeavesLegacyCheckpointsAlone(t *testing.T) {
	vars := map[string]any{"a": map[string]any{"x": 1.0}, "b": []any{"s"}}
	got := DecodeRefs(vars)
	if !Equal(got["a"], vars["a"]) || !Equal(got["b"], vars["b"]) {
		t.Fatalf("no markers → unchanged, got %#v", got)
	}
}
