package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestValidIdent(t *testing.T) {
	ok := []string{"mhl_store", "_x", "S3", "a1_b2"}
	bad := []string{"", "1table", "mhl store", "a.b", "drop;table", "tbl-1", `x"y`, "a."}
	for _, s := range ok {
		if !validIdent(s) {
			t.Errorf("validIdent(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if validIdent(s) {
			t.Errorf("validIdent(%q) = true, want false", s)
		}
	}
}

func TestLikeEscape(t *testing.T) {
	cases := map[string]string{
		"run/":        "run/",
		"a%b":         `a\%b`,
		"a_b":         `a\_b`,
		`a\b`:         `a\\b`,
		"100%_done\\": `100\%\_done\\`,
	}
	for in, want := range cases {
		if got := likeEscape(in); got != want {
			t.Errorf("likeEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildDSN(t *testing.T) {
	got := buildDSN("state/mhl.db", 5000, "WAL")
	want := "file:state/mhl.db?_pragma=busy_timeout%285000%29&_pragma=journal_mode%28WAL%29&_pragma=case_sensitive_like%28on%29"
	if got != want {
		t.Fatalf("buildDSN =\n %q\nwant\n %q", got, want)
	}
}

func TestCanonicalJSON(t *testing.T) {
	cases := map[string]string{
		`{"a":1, "b": [2, 3]}`: `{"a":1,"b":[2,3]}`,
		`"x"`:                  `"x"`,
		`7`:                    `7`,
		`not json`:             `not json`, // invalid input passes through unchanged
	}
	for in, want := range cases {
		if got := canonicalJSON([]byte(in)); string(got) != want {
			t.Errorf("canonicalJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewSQLiteStoreRequiresPath(t *testing.T) {
	if _, err := newSQLiteStore(context.Background(), sqliteConfig{}); err == nil {
		t.Fatal("expected an error for a missing `path`")
	}
}

func TestNewSQLiteStoreRejectsBadTable(t *testing.T) {
	_, err := newSQLiteStore(context.Background(), sqliteConfig{
		Path: filepath.Join(t.TempDir(), "x.db"), Table: "bad;name",
	})
	if err == nil {
		t.Fatal("expected an error for an invalid table identifier")
	}
}

func TestNewSQLiteStoreRejectsBadJournalMode(t *testing.T) {
	_, err := newSQLiteStore(context.Background(), sqliteConfig{
		Path: filepath.Join(t.TempDir(), "x.db"), JournalMode: "WAL;DROP",
	})
	if err == nil {
		t.Fatal("expected an error for an invalid journal_mode")
	}
}

func TestNewSQLiteStoreCreatesParentDirs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "a", "b", "mhl.db")
	s, err := newSQLiteStore(ctx, sqliteConfig{Path: path, AutoMigrate: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if err := s.put(ctx, "k", []byte(`1`)); err != nil {
		t.Fatal(err)
	}
}

// TestRoundTrip exercises get/put/delete/list, the ON CONFLICT upsert and
// the cas capability against a real database file — SQLite needs no external
// service, so the full suite always runs.
func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := newSQLiteStore(ctx, sqliteConfig{
		Path: filepath.Join(t.TempDir(), "mhl.db"), Table: "mhl_store_test",
		Prefix: "t/", AutoMigrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	if _, ok, err := s.get(ctx, "run/1"); err != nil || ok {
		t.Fatalf("get miss: ok=%v err=%v", ok, err)
	}
	if err := s.put(ctx, "run/1", []byte(`{"step":"gate"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.put(ctx, "run/1", []byte(`{"step":"review"}`)); err != nil { // upsert
		t.Fatal(err)
	}
	raw, ok, err := s.get(ctx, "run/1")
	if err != nil || !ok {
		t.Fatalf("get hit: ok=%v err=%v", ok, err)
	}
	var v map[string]any
	if json.Unmarshal(raw, &v); v["step"] != "review" {
		t.Fatalf("upsert did not overwrite: %v", v)
	}
	if err := s.put(ctx, "session/a", []byte(`"s"`)); err != nil {
		t.Fatal(err)
	}
	keys, err := s.list(ctx, "run/")
	if err != nil || len(keys) != 1 || keys[0] != "run/1" {
		t.Fatalf("list(run/) = %v err=%v", keys, err)
	}

	// LIKE metacharacters in the prefix are escaped, and LIKE is
	// case-sensitive (case_sensitive_like=on).
	if err := s.put(ctx, "100%/x", []byte(`1`)); err != nil {
		t.Fatal(err)
	}
	if err := s.put(ctx, "100XX/x", []byte(`1`)); err != nil { // '%' wildcard bait
		t.Fatal(err)
	}
	if keys, err := s.list(ctx, "100%/"); err != nil || len(keys) != 1 || keys[0] != "100%/x" {
		t.Fatalf("list(100%%/) = %v err=%v (LIKE escaping)", keys, err)
	}
	if keys, err := s.list(ctx, "RUN/"); err != nil || len(keys) != 0 {
		t.Fatalf("list(RUN/) = %v err=%v (LIKE must stay case-sensitive)", keys, err)
	}

	if err := s.del(ctx, "run/1"); err != nil {
		t.Fatal(err)
	}
	if err := s.del(ctx, "run/1"); err != nil {
		t.Fatalf("delete not idempotent: %v", err)
	}

	// --- cas capability -------------------------------------------------
	lockKey := "run/lk/lock"
	a := []byte(`{"holder":"A","expires":"t1"}`)
	b := []byte(`{"holder":"B","expires":"t2"}`)

	if ok, err := s.putIfAbsent(ctx, lockKey, a); err != nil || !ok {
		t.Fatalf("put_if_absent on a free key: ok=%v err=%v", ok, err)
	}
	if ok, err := s.putIfAbsent(ctx, lockKey, b); err != nil || ok {
		t.Fatalf("put_if_absent on a held key must not acquire: ok=%v err=%v", ok, err)
	}

	// Read it back the way the runtime does, then CAS on that exact value.
	cur, hit, err := s.get(ctx, lockKey)
	if err != nil || !hit {
		t.Fatalf("get(lock): hit=%v err=%v", hit, err)
	}
	if ok, err := s.compareAndSwap(ctx, lockKey, cur, b); err != nil || !ok {
		t.Fatalf("compare_and_swap on the current value: ok=%v err=%v", ok, err)
	}
	// Whitespace differences in `expected` must not matter (jsonb parity).
	if ok, err := s.compareAndSwap(ctx, lockKey, []byte(`{"holder": "B", "expires": "t2"}`), a); err != nil || !ok {
		t.Fatalf("compare_and_swap with non-canonical expected: ok=%v err=%v", ok, err)
	}
	// A stale expected value must fail.
	if ok, err := s.compareAndSwap(ctx, lockKey, b, []byte(`{"holder":"C"}`)); err != nil || ok {
		t.Fatalf("compare_and_swap on a stale value must not swap: ok=%v err=%v", ok, err)
	}
	// compare_and_swap on a missing key is a no-op miss, not an error.
	if ok, err := s.compareAndSwap(ctx, "run/nope/lock", b, a); err != nil || ok {
		t.Fatalf("compare_and_swap on a missing key: ok=%v err=%v", ok, err)
	}
}
