package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgStore is a `store`-kind backend on PostgreSQL: one row per key in a
// `(key text primary key, value jsonb, updated_at timestamptz)` table. `put`
// is an atomic `INSERT ... ON CONFLICT DO UPDATE`, so concurrent writes never
// corrupt a row; `list(prefix)` is an indexed `key LIKE prefix || '%'`.

const (
	defaultTable    = "mhl_store"
	defaultMaxConns = 8
	opTimeout       = 30 * time.Second // client-side guard per operation
)

type pgConfig struct {
	DSN string // full connection string (URL or keyword/value); wins over the discrete fields

	Host     string
	Port     string
	DBName   string
	User     string
	Password string
	SSLMode  string // default "prefer"

	Table            string // default "mhl_store"; may be "schema.table"
	Prefix           string // optional key namespace within the table
	MaxConns         int32  // default 8
	StatementTimeout time.Duration
	AutoMigrate      bool // default true
}

type pgStore struct {
	pool   *pgxpool.Pool
	table  string // validated identifier — safe to interpolate into SQL
	prefix string
}

func newPGStore(ctx context.Context, cfg pgConfig) (*pgStore, error) {
	table := cfg.Table
	if table == "" {
		table = defaultTable
	}
	if !validIdent(table) {
		return nil, fmt.Errorf("store-postgres: invalid `table` %q (identifier chars only; may be schema.table)", table)
	}

	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		dsn = buildKeywordDSN(cfg)
	}
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("store-postgres: bad connection config: %w", err)
	}
	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	} else {
		pcfg.MaxConns = defaultMaxConns
	}
	pcfg.MinConns = 0
	pcfg.MaxConnIdleTime = 5 * time.Minute
	if cfg.StatementTimeout > 0 {
		if pcfg.ConnConfig.RuntimeParams == nil {
			pcfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		pcfg.ConnConfig.RuntimeParams["statement_timeout"] =
			strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("store-postgres: connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store-postgres: ping: %w", err)
	}

	s := &pgStore{pool: pool, table: table, prefix: cfg.Prefix}
	if cfg.AutoMigrate {
		if err := s.migrate(ctx); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *pgStore) migrate(ctx context.Context) error {
	// s.table passed validIdent, so interpolation here is safe.
	create := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		key        text PRIMARY KEY,
		value      jsonb NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now()
	)`, s.table)
	index := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_key_pattern_idx ON %s (key text_pattern_ops)`,
		unqualify(s.table), s.table)
	for _, q := range []string{create, index} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("store-postgres: migrate: %w", err)
		}
	}
	return nil
}

func (s *pgStore) key(logical string) string { return s.prefix + logical }

func (s *pgStore) get(ctx context.Context, logical string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	var raw []byte
	err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT value FROM %s WHERE key = $1`, s.table), s.key(logical)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("store-postgres: get %q: %w", logical, err)
	}
	return raw, true, nil
}

func (s *pgStore) put(ctx context.Context, logical string, value []byte) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (key, value, updated_at) VALUES ($1, $2::jsonb, now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, s.table),
		s.key(logical), string(value))
	if err != nil {
		return fmt.Errorf("store-postgres: put %q: %w", logical, err)
	}
	return nil
}

func (s *pgStore) del(ctx context.Context, logical string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if _, err := s.pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE key = $1`, s.table), s.key(logical)); err != nil {
		return fmt.Errorf("store-postgres: delete %q: %w", logical, err)
	}
	return nil
}

func (s *pgStore) list(ctx context.Context, logicalPrefix string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	pattern := likeEscape(s.prefix+logicalPrefix) + "%"
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT key FROM %s WHERE key LIKE $1 ESCAPE '\' ORDER BY key`, s.table), pattern)
	if err != nil {
		return nil, fmt.Errorf("store-postgres: list: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("store-postgres: list: %w", err)
		}
		out = append(out, strings.TrimPrefix(k, s.prefix))
	}
	return out, rows.Err()
}

func (s *pgStore) close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// --- helpers -------------------------------------------------------------

// validIdent accepts a bare SQL identifier or a schema-qualified one
// ("schema.table"): each part starts with a letter or underscore and
// continues with letters, digits or underscores.
func validIdent(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 2 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for i, r := range p {
			switch {
			case r == '_', unicode.IsLetter(r):
			case i > 0 && unicode.IsDigit(r):
			default:
				return false
			}
		}
	}
	return true
}

func unqualify(table string) string {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		return table[i+1:]
	}
	return table
}

// likeEscape neutralises LIKE metacharacters in a literal prefix (ESCAPE '\').
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// buildKeywordDSN assembles a libpq keyword/value connection string from the
// discrete properties. pgxpool.ParseConfig accepts this form as well as URLs.
func buildKeywordDSN(c pgConfig) string {
	var b strings.Builder
	add := func(k, v string) {
		if v == "" {
			return
		}
		if strings.ContainsAny(v, " '\\") {
			v = "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
		}
		fmt.Fprintf(&b, "%s=%s ", k, v)
	}
	add("host", c.Host)
	add("port", c.Port)
	add("dbname", c.DBName)
	add("user", c.User)
	add("password", c.Password)
	sslmode := c.SSLMode
	if sslmode == "" {
		sslmode = "prefer"
	}
	add("sslmode", sslmode)
	return strings.TrimSpace(b.String())
}
