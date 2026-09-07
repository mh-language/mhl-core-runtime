package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestValidIdent(t *testing.T) {
	ok := []string{"mhl_store", "_x", "public.mhl_store", "S3", "a1_b2"}
	bad := []string{"", "1table", "mhl store", "a.b.c", "drop;table", "tbl-1", `x"y`, "a."}
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

func TestUnqualify(t *testing.T) {
	if got := unqualify("public.mhl_store"); got != "mhl_store" {
		t.Errorf("got %q", got)
	}
	if got := unqualify("mhl_store"); got != "mhl_store" {
		t.Errorf("got %q", got)
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

func TestBuildKeywordDSN(t *testing.T) {
	got := buildKeywordDSN(pgConfig{
		Host: "db.internal", Port: "5432", DBName: "mhl_state",
		User: "mhl", Password: "p w'x",
	})
	want := `host=db.internal port=5432 dbname=mhl_state user=mhl password='p w\'x' sslmode=prefer`
	if got != want {
		t.Fatalf("buildKeywordDSN =\n %q\nwant\n %q", got, want)
	}
}

func TestDSNTargetHidesPassword(t *testing.T) {
	got := dsnTarget(pgConfig{DSN: "postgres://mhl:secret@db:5432/mhl_state?sslmode=disable"})
	if got != "db:5432/mhl_state?sslmode=disable" {
		t.Fatalf("dsnTarget leaked or mangled: %q", got)
	}
	if got := dsnTarget(pgConfig{Host: "h", Port: "5432", DBName: "d"}); got != "h:5432/d" {
		t.Fatalf("dsnTarget(discrete) = %q", got)
	}
}

func TestPortString(t *testing.T) {
	if portString("5432") != "5432" || portString(float64(5432)) != "5432" || portString(json.Number("5432")) != "5432" {
		t.Fatal("portString mishandled a form")
	}
	if portString(nil) != "" {
		t.Fatal("portString(nil) should be empty")
	}
}

func TestNewPGStoreRejectsBadTable(t *testing.T) {
	_, err := newPGStore(context.Background(), pgConfig{DSN: "postgres://x@y/z", Table: "bad;name"})
	if err == nil {
		t.Fatal("expected an error for an invalid table identifier")
	}
}

// TestRoundTripAgainstRealPostgres runs only when MHL_PG_TEST_DSN points at a
// reachable database (the CENARIO suite and `make smoke` cover this path in
// CI-less runs). It exercises get/put/delete/list and the ON CONFLICT upsert.
func TestRoundTripAgainstRealPostgres(t *testing.T) {
	dsn := os.Getenv("MHL_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("set MHL_PG_TEST_DSN to run the live Postgres round trip")
	}
	ctx := context.Background()
	s, err := newPGStore(ctx, pgConfig{
		DSN: dsn, Table: "mhl_store_test", Prefix: "t/", AutoMigrate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = s.pool.Exec(ctx, "DROP TABLE IF EXISTS mhl_store_test")
		s.close()
	}()

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
	if err := s.del(ctx, "run/1"); err != nil {
		t.Fatal(err)
	}
	if err := s.del(ctx, "run/1"); err != nil {
		t.Fatalf("delete not idempotent: %v", err)
	}
	if keys, _ := s.list(ctx, ""); len(keys) != 1 || keys[0] != "session/a" {
		t.Fatalf("after cleanup, list() = %v", keys)
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
	// A stale expected value must fail.
	if ok, err := s.compareAndSwap(ctx, lockKey, a, []byte(`{"holder":"C"}`)); err != nil || ok {
		t.Fatalf("compare_and_swap on a stale value must not swap: ok=%v err=%v", ok, err)
	}
	// compare_and_swap on a missing key is a no-op miss, not an error.
	if ok, err := s.compareAndSwap(ctx, "run/nope/lock", a, b); err != nil || ok {
		t.Fatalf("compare_and_swap on a missing key: ok=%v err=%v", ok, err)
	}
	_ = s.del(ctx, lockKey)
	_ = s.del(ctx, "session/a")

	// --- claim capability (SELECT … FOR UPDATE SKIP LOCKED) ------------
	_ = s.put(ctx, "run/c-new/status", []byte(`{"tool":"P","state":"pending","startedAt":"2026-09-06T10:02:00Z"}`))
	_ = s.put(ctx, "run/c-old/status", []byte(`{"tool":"P","state":"pending","startedAt":"2026-09-06T10:01:00Z"}`))
	_ = s.put(ctx, "run/c-run/status", []byte(`{"tool":"P","state":"working","startedAt":"2026-09-06T10:00:00Z"}`))

	// Oldest pending first, flipped to claimed for the holder.
	key, val, ok, err := s.claimNext(ctx, "run/", "replica-A")
	if err != nil || !ok || key != "run/c-old/status" {
		t.Fatalf("claim_next #1 = key:%q ok:%v err:%v", key, ok, err)
	}
	var cr map[string]any
	_ = json.Unmarshal(val, &cr)
	if cr["state"] != "claimed" || cr["holder"] != "replica-A" {
		t.Fatalf("claim_next #1 value = %v", cr)
	}
	// Second claim takes the remaining pending; third finds none.
	if key, _, ok, _ := s.claimNext(ctx, "run/", "replica-B"); !ok || key != "run/c-new/status" {
		t.Fatalf("claim_next #2 = key:%q ok:%v", key, ok)
	}
	if _, _, ok, err := s.claimNext(ctx, "run/", "replica-B"); ok || err != nil {
		t.Fatalf("claim_next #3 should find nothing: ok=%v err=%v", ok, err)
	}
	// The `working` row was never touched.
	if raw, _, _ := s.get(ctx, "run/c-run/status"); string(raw) == "" {
		t.Fatal("working row vanished")
	}
	for _, k := range []string{"run/c-new/status", "run/c-old/status", "run/c-run/status"} {
		_ = s.del(ctx, k)
	}

	// --- fence capability (atomic put_fenced / delete_fenced) ---------
	lk := "run/fx/lock"
	_ = s.put(ctx, lk, []byte(`{"holder":"A","token":"t1","expires":"2999-01-01T00:00:00Z"}`))
	ckKey := "run/fx/checkpoint/P"

	// Lease held by A/t1 → the write lands.
	if w, err := s.putFenced(ctx, ckKey, []byte(`{"step":"S1"}`), lk, "A", "t1"); err != nil || !w {
		t.Fatalf("put_fenced with the lease held: written=%v err=%v", w, err)
	}
	// Wrong token → rejected, and the checkpoint is unchanged.
	if w, err := s.putFenced(ctx, ckKey, []byte(`{"step":"ZOMBIE"}`), lk, "A", "stale"); err != nil || w {
		t.Fatalf("put_fenced with a stale token must not write: written=%v err=%v", w, err)
	}
	if raw, _, _ := s.get(ctx, ckKey); string(raw) != `{"step": "S1"}` && string(raw) != `{"step":"S1"}` {
		t.Fatalf("checkpoint mutated by a fenced-out write: %s", raw)
	}
	// After takeover (lease now B/t2) A's writes are rejected; B's land.
	_ = s.put(ctx, lk, []byte(`{"holder":"B","token":"t2","expires":"2999-01-01T00:00:00Z"}`))
	if w, _ := s.putFenced(ctx, ckKey, []byte(`{"step":"ZOMBIE"}`), lk, "A", "t1"); w {
		t.Fatal("deposed A still wrote a checkpoint")
	}
	if w, err := s.putFenced(ctx, ckKey, []byte(`{"step":"S2"}`), lk, "B", "t2"); err != nil || !w {
		t.Fatalf("B (the new holder) put_fenced: written=%v err=%v", w, err)
	}
	// delete_fenced: A rejected, B ok (idempotent even once the key is gone).
	if d, _ := s.deleteFenced(ctx, ckKey, lk, "A", "t1"); d {
		t.Fatal("deposed A deleted a checkpoint")
	}
	if d, err := s.deleteFenced(ctx, ckKey, lk, "B", "t2"); err != nil || !d {
		t.Fatalf("B delete_fenced: deleted=%v err=%v", d, err)
	}
	if d, err := s.deleteFenced(ctx, ckKey, lk, "B", "t2"); err != nil || !d {
		t.Fatalf("B delete_fenced on an absent key (lease held) must still be ok: %v %v", d, err)
	}
	_ = s.del(ctx, lk)

	// --- scan capability (list_statuses / count_pending) --------------
	_ = s.put(ctx, "run/s-a/status", []byte(`{"tool":"P","state":"pending","startedAt":"2026-09-06T10:00:00Z"}`))
	_ = s.put(ctx, "run/s-b/status", []byte(`{"tool":"P","state":"working","startedAt":"2026-09-06T10:01:00Z"}`))
	_ = s.put(ctx, "run/s-c/status", []byte(`{"tool":"P","state":"pending","startedAt":"2026-09-06T10:02:00Z"}`))
	_ = s.put(ctx, "run/s-a/owner", []byte(`"owner-hash"`))
	_ = s.put(ctx, "run/s-a/checkpoint/P", []byte(`{"pipeline":"P"}`))

	recs, err := s.listStatuses(ctx, "run/")
	if err != nil {
		t.Fatalf("list_statuses: %v", err)
	}
	if len(recs) != 3 || recs["run/s-a/status"] == nil || recs["run/s-b/status"] == nil {
		t.Fatalf("list_statuses returned %d rows: %v", len(recs), keysOf(recs))
	}
	if _, leaked := recs["run/s-a/owner"]; leaked {
		t.Fatal("list_statuses leaked a non-status row")
	}

	n, err := s.countPending(ctx, "run/")
	if err != nil || n != 2 {
		t.Fatalf("count_pending = %d, err=%v (want 2)", n, err)
	}
	for _, k := range []string{"run/s-a/status", "run/s-b/status", "run/s-c/status", "run/s-a/owner", "run/s-a/checkpoint/P"} {
		_ = s.del(ctx, k)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
