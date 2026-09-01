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
}
