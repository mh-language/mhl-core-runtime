package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

// sqliteStore is a `store`-kind backend on SQLite: one row per key in a
// `(key TEXT PRIMARY KEY, value TEXT, updated_at TIMESTAMP)` table inside a
// local file. `put` is an atomic `INSERT ... ON CONFLICT DO UPDATE`, so
// concurrent writers never corrupt a row; `list(prefix)` is a
// `key LIKE prefix || '%'` over the primary-key index.

const (
	defaultTable         = "mhl_store"
	defaultBusyTimeoutMS = 5000
	defaultJournalMode   = "WAL"
	opTimeout            = 30 * time.Second // client-side guard per operation
)

type sqliteConfig struct {
	Path string // database file — required

	Table  string // default "mhl_store"
	Prefix string // optional key namespace within the table

	BusyTimeoutMS int    // default 5000 — wait on a locked database before SQLITE_BUSY
	JournalMode   string // default "WAL"
	AutoMigrate   bool   // default true
}

type sqliteStore struct {
	db     *sql.DB
	table  string // validated identifier — safe to interpolate into SQL
	prefix string
}

func newSQLiteStore(ctx context.Context, cfg sqliteConfig) (*sqliteStore, error) {
	table := cfg.Table
	if table == "" {
		table = defaultTable
	}
	if !validIdent(table) {
		return nil, fmt.Errorf("store-sqlite: invalid `table` %q (identifier chars only)", table)
	}

	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return nil, fmt.Errorf("store-sqlite: `path` is required (the SQLite database file, e.g. \"state/mhl.db\")")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store-sqlite: create directory for %q: %w", path, err)
		}
	}

	busy := cfg.BusyTimeoutMS
	if busy <= 0 {
		busy = defaultBusyTimeoutMS
	}
	journal := cfg.JournalMode
	if journal == "" {
		journal = defaultJournalMode
	}
	// The journal mode lands inside the DSN, so it must come from a fixed
	// allow-list rather than arbitrary text.
	switch strings.ToUpper(journal) {
	case "DELETE", "TRUNCATE", "PERSIST", "MEMORY", "WAL", "OFF":
	default:
		return nil, fmt.Errorf("store-sqlite: invalid `journal_mode` %q (DELETE | TRUNCATE | PERSIST | MEMORY | WAL | OFF)", journal)
	}

	dsn := buildDSN(path, busy, strings.ToUpper(journal))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store-sqlite: open %q: %w", path, err)
	}
	// A single connection serialises writers — the reliable way to avoid
	// SQLITE_BUSY under concurrent calls (busy_timeout alone only retries).
	db.SetMaxOpenConns(1)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store-sqlite: ping %q: %w", path, err)
	}

	s := &sqliteStore{db: db, table: table, prefix: cfg.Prefix}
	if cfg.AutoMigrate {
		if err := s.migrate(ctx); err != nil {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *sqliteStore) migrate(ctx context.Context) error {
	// s.table passed validIdent, so interpolation here is safe.
	create := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`, s.table)
	if _, err := s.db.ExecContext(ctx, create); err != nil {
		return fmt.Errorf("store-sqlite: migrate: %w", err)
	}
	return nil
}

func (s *sqliteStore) key(logical string) string { return s.prefix + logical }

func (s *sqliteStore) get(ctx context.Context, logical string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	var raw string
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT value FROM %s WHERE key = ?`, s.table), s.key(logical)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("store-sqlite: get %q: %w", logical, err)
	}
	return []byte(raw), true, nil
}

func (s *sqliteStore) put(ctx context.Context, logical string, value []byte) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, s.table),
		s.key(logical), string(canonicalJSON(value)))
	if err != nil {
		return fmt.Errorf("store-sqlite: put %q: %w", logical, err)
	}
	return nil
}

// putIfAbsent inserts value at key only when no row exists yet. acquired is
// false (nil error) when a row is already present. One atomic statement
// (`INSERT ... ON CONFLICT DO NOTHING`), which is what the cross-replica run
// lock's acquire step needs.
func (s *sqliteStore) putIfAbsent(ctx context.Context, logical string, value []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT (key) DO NOTHING`, s.table),
		s.key(logical), string(canonicalJSON(value)))
	if err != nil {
		return false, fmt.Errorf("store-sqlite: put_if_absent %q: %w", logical, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store-sqlite: put_if_absent %q: %w", logical, err)
	}
	return n == 1, nil
}

// compareAndSwap replaces value at key with newValue only when the current
// row's value equals expected, compared as canonical JSON text (values are
// stored as compact JSON, and expected is canonicalised the same way, so
// whitespace does not matter). swapped is false (nil error) on a value
// mismatch or a missing row. One atomic `UPDATE ... WHERE ... AND value = ?`.
func (s *sqliteStore) compareAndSwap(ctx context.Context, logical string, expected, newValue []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET value = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE key = ? AND value = ?`, s.table),
		string(canonicalJSON(newValue)), s.key(logical), string(canonicalJSON(expected)))
	if err != nil {
		return false, fmt.Errorf("store-sqlite: compare_and_swap %q: %w", logical, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store-sqlite: compare_and_swap %q: %w", logical, err)
	}
	return n == 1, nil
}

func (s *sqliteStore) del(ctx context.Context, logical string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if _, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE key = ?`, s.table), s.key(logical)); err != nil {
		return fmt.Errorf("store-sqlite: delete %q: %w", logical, err)
	}
	return nil
}

func (s *sqliteStore) list(ctx context.Context, logicalPrefix string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	pattern := likeEscape(s.prefix+logicalPrefix) + "%"
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT key FROM %s WHERE key LIKE ? ESCAPE '\' ORDER BY key`, s.table), pattern)
	if err != nil {
		return nil, fmt.Errorf("store-sqlite: list: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("store-sqlite: list: %w", err)
		}
		out = append(out, strings.TrimPrefix(k, s.prefix))
	}
	return out, rows.Err()
}

func (s *sqliteStore) close() {
	if s.db != nil {
		s.db.Close()
	}
}

// --- helpers -------------------------------------------------------------

// buildDSN assembles the modernc.org/sqlite connection string: the `file:`
// form with `_pragma` parameters. `case_sensitive_like(on)` keeps LIKE
// case-sensitive so list(prefix) behaves like the other stores.
func buildDSN(path string, busyTimeoutMS int, journalMode string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout("+strconv.Itoa(busyTimeoutMS)+")")
	q.Add("_pragma", "journal_mode("+journalMode+")")
	q.Add("_pragma", "case_sensitive_like(on)")
	return "file:" + path + "?" + q.Encode()
}

// canonicalJSON rewrites raw as the exact compact form json.Marshal of the
// decoded value produces — the form every stored value has (put, putIfAbsent
// and compareAndSwap all canonicalise on write), so CAS is a plain text
// comparison insensitive to whitespace and object key order. Numbers decode
// as json.Number so large integers survive the round trip byte-identically.
// Raw text that isn't valid JSON is returned unchanged (it can then only
// match an identically stored row).
func canonicalJSON(raw []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return b
}

// validIdent accepts a bare SQL identifier: starts with a letter or
// underscore and continues with letters, digits or underscores. (Unlike the
// Postgres backend, schema qualification is not meaningful here.)
func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', unicode.IsLetter(r):
		case i > 0 && unicode.IsDigit(r):
		default:
			return false
		}
	}
	return true
}

// likeEscape neutralises LIKE metacharacters in a literal prefix (ESCAPE '\').
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
