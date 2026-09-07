package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeValue(t *testing.T) {
	uuid := [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	if got := normalizeValue(uuid); got != "12345678-9abc-def0-1122-334455667788" {
		t.Errorf("uuid = %v", got)
	}
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := normalizeValue(ts); got != "2026-01-02T03:04:05Z" {
		t.Errorf("time = %v", got)
	}
	nested := []any{[16]byte{}, map[string]any{"t": ts}}
	got := normalizeValue(nested).([]any)
	if got[0] != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("nested uuid = %v", got[0])
	}
	if got[1].(map[string]any)["t"] != "2026-01-02T03:04:05Z" {
		t.Errorf("nested time = %v", got[1])
	}
	for _, v := range []any{nil, int64(7), "x", true, 3.5} {
		if normalizeValue(v) != v {
			t.Errorf("passthrough changed %v", v)
		}
	}
}

func TestSqlAndArgs(t *testing.T) {
	p := callParams{Args: []any{"SELECT $1", "a", 2.0}}
	s, args, err := p.sqlAndArgs()
	if err != nil || s != "SELECT $1" || !reflect.DeepEqual(args, []any{"a", 2.0}) {
		t.Fatalf("positional: s=%q args=%v err=%v", s, args, err)
	}

	p = callParams{NamedArgs: map[string]any{"sql": "SELECT 1", "args": []any{"z"}}}
	s, args, err = p.sqlAndArgs()
	if err != nil || s != "SELECT 1" || !reflect.DeepEqual(args, []any{"z"}) {
		t.Fatalf("named: s=%q args=%v err=%v", s, args, err)
	}

	if _, _, err := (callParams{}).sqlAndArgs(); err == nil {
		t.Fatal("expected an error with no SQL")
	}
	if _, _, err := (callParams{Args: []any{42}}).sqlAndArgs(); err == nil {
		t.Fatal("expected an error when the first arg is not a string")
	}
}

func TestBuildKeywordDSNAndTarget(t *testing.T) {
	c := sqlConfig{Host: "db", Port: "5432", DBName: "demo", User: "mhl", Password: "p w"}
	if got := buildKeywordDSN(c); got != `host=db port=5432 dbname=demo user=mhl password='p w' sslmode=prefer` {
		t.Fatalf("dsn = %q", got)
	}
	if got := dsnTarget(sqlConfig{DSN: "postgres://u:secret@h:5432/demo"}); got != "h:5432/demo" {
		t.Fatalf("target leaked/mangled: %q", got)
	}
	if got := dsnTarget(c); got != "db:5432/demo" {
		t.Fatalf("target(discrete) = %q", got)
	}
}

func TestHead(t *testing.T) {
	if got := head("  SELECT   a,\n b\tFROM x  "); got != "SELECT a, b FROM x" {
		t.Fatalf("head = %q", got)
	}
	long := head(string(make([]byte, 200)))
	if len(long) != 80 {
		t.Fatalf("head not capped: %d", len(long))
	}
}

// TestLiveDQL runs only when MHL_SQL_PG_TEST_DSN points at a database seeded
// with initdb/seed.sql (the CENARIO suite and `make smoke` cover this without
// CI). It exercises query/queryRow/queryValue, type mapping, $-params, and the
// read-only guard.
func TestLiveDQL(t *testing.T) {
	dsn := os.Getenv("MHL_SQL_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("set MHL_SQL_PG_TEST_DSN (db seeded with initdb/seed.sql) to run the live test")
	}
	ctx := context.Background()

	ro, err := newSQLDB(ctx, sqlConfig{DSN: dsn, ReadOnly: true, MaxRows: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer ro.close()

	rows, err := ro.query(ctx, "SELECT name, score, tags FROM people WHERE org = $1 ORDER BY name", []any{"acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0]["name"] != "Ana" {
		t.Fatalf("rows = %v", rows)
	}
	if b, _ := json.Marshal(rows[0]["tags"]); string(b) != `["lead","eu"]` {
		t.Fatalf("jsonb tags = %s", b)
	}

	one, err := ro.queryRow(ctx, "SELECT name FROM people ORDER BY score DESC LIMIT 1", nil)
	if err != nil || one["name"] != "Ana" {
		t.Fatalf("queryRow = %v err=%v", one, err)
	}
	if miss, err := ro.queryRow(ctx, "SELECT 1 WHERE false", nil); err != nil || miss != nil {
		t.Fatalf("queryRow miss = %v err=%v", miss, err)
	}
	n, err := ro.queryValue(ctx, "SELECT count(*) FROM people WHERE active", nil)
	if err != nil || n.(int64) != 4 {
		t.Fatalf("queryValue = %v err=%v", n, err)
	}

	// max_rows guard (org=acme has 3 rows, limit is 3 -> the 4th check trips
	// only for a bigger set):
	if _, err := ro.query(ctx, "SELECT * FROM people", nil); err == nil {
		t.Fatal("expected max_rows to trip on the full table (5 rows > 3)")
	}

	// read-only: a write is rejected by PostgreSQL, and exec is disabled.
	if _, err := ro.query(ctx, "INSERT INTO people(name, org) VALUES ('x','y')", nil); err == nil {
		t.Fatal("write via query() succeeded under read_only")
	}
	if _, err := ro.exec(ctx, "INSERT INTO people(name, org) VALUES ('x','y')", nil); err == nil {
		t.Fatal("exec() not disabled under read_only")
	}

	if _, err := ro.execScript(ctx, "CREATE TABLE z(i int)"); err == nil {
		t.Fatal("execScript() not disabled under read_only")
	}

	// read_only:false lets exec / execScript write; clean up after.
	rw, err := newSQLDB(ctx, sqlConfig{DSN: dsn, ReadOnly: false})
	if err != nil {
		t.Fatal(err)
	}
	defer rw.close()
	if aff, err := rw.exec(ctx, "INSERT INTO people(name, org) VALUES ('tmp','tmp')", nil); err != nil || aff != 1 {
		t.Fatalf("exec insert: aff=%d err=%v", aff, err)
	}
	if _, err := rw.exec(ctx, "DELETE FROM people WHERE org = 'tmp'", nil); err != nil {
		t.Fatal(err)
	}

	// execScript: multi-statement DDL commits atomically.
	if _, err := rw.execScript(ctx, `
		CREATE TABLE ddl_probe (id int PRIMARY KEY);
		CREATE INDEX ddl_probe_i ON ddl_probe (id);
		INSERT INTO ddl_probe VALUES (1), (2);
	`); err != nil {
		t.Fatalf("execScript apply: %v", err)
	}
	if n, _ := rw.queryValue(ctx, "SELECT count(*) FROM ddl_probe", nil); n.(int64) != 2 {
		t.Fatalf("execScript rows = %v", n)
	}
	// a script that fails midway rolls back entirely.
	_, err = rw.execScript(ctx, `
		ALTER TABLE ddl_probe ADD COLUMN note text;
		INSERT INTO ddl_probe (id, bad_col) VALUES (3, 'x');
	`)
	if err == nil {
		t.Fatal("execScript with a bad statement did not error")
	}
	if col, _ := rw.queryValue(ctx,
		"SELECT count(*) FROM information_schema.columns WHERE table_name='ddl_probe' AND column_name='note'", nil); col.(int64) != 0 {
		t.Fatal("execScript did not roll back the ADD COLUMN")
	}
	if _, err := rw.exec(ctx, "DROP TABLE ddl_probe", nil); err != nil {
		t.Fatal(err)
	}
}
