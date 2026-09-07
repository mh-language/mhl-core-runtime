package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sqlDB runs free-form data queries (DQL) against PostgreSQL and returns rows
// as JSON-friendly objects. "DQL only" is enforced in depth:
//
//   - the pool sets `default_transaction_read_only = on`, so PostgreSQL itself
//     rejects any write (INSERT/UPDATE/DELETE/SELECT ... FOR UPDATE/DDL);
//   - queries always go through the extended protocol (`conn.Query(sql,
//     args...)`), which forbids multiple statements in one string;
//   - the caller passes values as positional args bound to `$1, $2, …` — the
//     workflow never interpolates a value into the SQL text.
//
// `exec` (DML) is available only when `read_only` is explicitly false.

const (
	defaultMaxConns = 4
	defaultMaxRows  = 10000
	opTimeout       = 30 * time.Second
	scriptTimeout   = 5 * time.Minute // execScript may be a real migration
)

type sqlConfig struct {
	DSN string

	Host     string
	Port     string
	DBName   string
	User     string
	Password string
	SSLMode  string

	ReadOnly         bool
	MaxRows          int
	MaxConns         int32
	StatementTimeout time.Duration
}

type sqlDB struct {
	pool     *pgxpool.Pool
	readOnly bool
	maxRows  int
}

func newSQLDB(ctx context.Context, cfg sqlConfig) (*sqlDB, error) {
	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		dsn = buildKeywordDSN(cfg)
	}
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("sql-postgres: bad connection config: %w", err)
	}
	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	} else {
		pcfg.MaxConns = defaultMaxConns
	}
	pcfg.MinConns = 0
	pcfg.MaxConnIdleTime = 5 * time.Minute

	if pcfg.ConnConfig.RuntimeParams == nil {
		pcfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	if cfg.ReadOnly {
		pcfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	if cfg.StatementTimeout > 0 {
		pcfg.ConnConfig.RuntimeParams["statement_timeout"] =
			strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("sql-postgres: connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("sql-postgres: ping: %w", err)
	}

	maxRows := cfg.MaxRows
	if maxRows == 0 {
		maxRows = defaultMaxRows
	}
	return &sqlDB{pool: pool, readOnly: cfg.ReadOnly, maxRows: maxRows}, nil
}

func (d *sqlDB) close() {
	if d.pool != nil {
		d.pool.Close()
	}
}

// query runs a SELECT and returns every row as a map keyed by column name.
func (d *sqlDB) query(ctx context.Context, sql string, args []any) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	rows, err := d.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, wrapPGError(err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	out := make([]map[string]any, 0, 16)
	for rows.Next() {
		if d.maxRows > 0 && len(out) >= d.maxRows {
			return nil, fmt.Errorf("sql-postgres: result exceeded max_rows=%d — add a LIMIT (or raise max_rows)", d.maxRows)
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, wrapPGError(err)
		}
		row := make(map[string]any, len(fields))
		for i, f := range fields {
			row[string(f.Name)] = normalizeValue(vals[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapPGError(err)
	}
	return out, nil
}

// queryRow returns the first row, or nil when the query yields none.
func (d *sqlDB) queryRow(ctx context.Context, sql string, args []any) (map[string]any, error) {
	rows, err := d.query(ctx, sql, args)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// queryValue returns the first column of the first row, or nil.
func (d *sqlDB) queryValue(ctx context.Context, sql string, args []any) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	rows, err := d.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, wrapPGError(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	vals, err := rows.Values()
	if err != nil {
		return nil, wrapPGError(err)
	}
	if len(vals) == 0 {
		return nil, nil
	}
	return normalizeValue(vals[0]), nil
}

// exec runs a single DML/DDL statement and returns the affected-row count.
// Refused unless read_only was explicitly set to false.
func (d *sqlDB) exec(ctx context.Context, sql string, args []any) (int64, error) {
	if d.readOnly {
		return 0, fmt.Errorf("sql-postgres: exec is disabled (read_only: true) — this extension is for DQL")
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	tag, err := d.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, wrapPGError(err)
	}
	return tag.RowsAffected(), nil
}

// execScript runs a multi-statement DDL/DML script inside a single
// transaction: every statement commits together, or none do (rollback on the
// first error). No bind parameters — statement-level $1 does not span a
// script. Refused unless read_only is false. Note: a few statements cannot run
// inside a transaction (CREATE INDEX CONCURRENTLY, VACUUM, CREATE DATABASE) —
// use `exec` for those.
func (d *sqlDB) execScript(ctx context.Context, script string) (int64, error) {
	if d.readOnly {
		return 0, fmt.Errorf("sql-postgres: execScript is disabled (read_only: true)")
	}
	ctx, cancel := context.WithTimeout(ctx, scriptTimeout)
	defer cancel()

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, wrapPGError(err)
	}
	defer tx.Rollback(ctx) // no-op once Commit succeeds

	tag, err := tx.Exec(ctx, script) // no args -> simple protocol -> multi-statement
	if err != nil {
		return 0, wrapPGError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, wrapPGError(err)
	}
	return tag.RowsAffected(), nil
}

// normalizeValue turns a pgx-decoded value into something json.Marshal renders
// well. pgx already yields Go natives and pgtype values that JSON-marshal
// sensibly (numeric -> number, jsonb -> nested value); the two it gets wrong
// are raw UUID bytes (a JSON int array) and time (kept explicit as RFC3339).
func normalizeValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case [16]byte:
		return formatUUID(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	case []any:
		for i := range t {
			t[i] = normalizeValue(t[i])
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = normalizeValue(t[k])
		}
		return t
	default:
		return v
	}
}

func formatUUID(b [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// wrapPGError attaches the SQLSTATE to the message when there is one, so a
// read-only rejection (25006) or a syntax error (42601) is legible in the
// workflow's failure.
func wrapPGError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return fmt.Errorf("sql-postgres: %s (SQLSTATE %s)", pg.Message, pg.Code)
	}
	return fmt.Errorf("sql-postgres: %w", err)
}

// --- connection-string helpers (shared shape with mhl-store-postgres) -----

func buildKeywordDSN(c sqlConfig) string {
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

func dsnTarget(c sqlConfig) string {
	if c.DSN != "" {
		if i := strings.Index(c.DSN, "@"); i >= 0 {
			return c.DSN[i+1:]
		}
		return "(dsn)"
	}
	host := c.Host
	if host == "" {
		host = "localhost"
	}
	if c.Port != "" {
		host += ":" + c.Port
	}
	return host + "/" + c.DBName
}
