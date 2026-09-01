// mhl-sql-postgres is an official `sql`-kind mhl extension: it runs free-form
// data queries (DQL) against PostgreSQL from a `.mh` workflow and returns rows
// as JSON objects.
//
// It is NOT the `store` KV backend (see src/mhl-store-postgres). Different
// kind, so both can be installed at once.
//
//	extension sql Db {
//	    dsn:               env("DATABASE_URL")   // or host/port/dbname/user/password
//	    read_only:         true                  // default — DQL only; PG rejects writes
//	    max_rows:          10000                 // guard against unbounded SELECTs
//	    statement_timeout: "10s"
//	    max_conns:         4
//	}
//
//	pipeline Enrich {
//	    step pull {
//	        var users = Db.query(
//	            "SELECT id, email, plan FROM users WHERE org = $1 AND active = $2",
//	            orgId, true)
//	        log(users)   // [{"id":1,"email":"...","plan":"pro"}, ...]
//	    }
//	}
//
// Methods: query(sql, ...args) -> [object] · queryRow -> object|null ·
// queryValue -> any · exec(sql, ...args) -> int (single statement) ·
// execScript(sql) -> int (multi-statement DDL/DML in one transaction). exec and
// execScript work only when read_only is false. The first arg is the SQL; the
// rest bind to $1, $2, … — a value is never interpolated into the SQL text.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	extID      = "dev.mhl.sql-postgres"
	extVersion = "0.1.0"
	apiVersion = "1"
)

type rpc struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

type callParams struct {
	Declaration struct {
		Name  string `json:"name"`
		Props []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"props"`
	} `json:"declaration"`
	Operation string         `json:"operation"`
	Args      []any          `json:"args"`
	NamedArgs map[string]any `json:"named_args"`
}

// sqlAndArgs pulls the SQL text and the bind parameters out of a call: the
// `.mh` path sends them positionally (sql first), and a `{sql, args}` named
// form is accepted too.
func (p callParams) sqlAndArgs() (string, []any, error) {
	if s, ok := p.NamedArgs["sql"].(string); ok {
		args, _ := p.NamedArgs["args"].([]any)
		return s, args, nil
	}
	if len(p.Args) == 0 {
		return "", nil, fmt.Errorf("missing SQL text (first argument)")
	}
	s, ok := p.Args[0].(string)
	if !ok {
		return "", nil, fmt.Errorf("first argument must be the SQL string")
	}
	return s, p.Args[1:], nil
}

func main() {
	s := &server{}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 8<<20)

	var outMu sync.Mutex
	enc := json.NewEncoder(os.Stdout)
	send := func(m rpc) { outMu.Lock(); _ = enc.Encode(m); outMu.Unlock() }

	var wg sync.WaitGroup
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var msg rpc
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			send(rpc{ID: msg.ID, Result: map[string]any{
				"api_version": apiVersion,
				"extension":   map[string]string{"id": extID, "version": extVersion},
			}})
		case "call":
			var p callParams
			_ = json.Unmarshal(msg.Params, &p)
			s.config(p)

			wg.Add(1)
			go func() {
				defer wg.Done()
				send(s.handleCall(msg))
			}()
		case "shutdown":
			wg.Wait()
			s.shutdown("notify")
			return
		}
	}
	wg.Wait()
	s.shutdown("eof")
}

type server struct {
	once   sync.Once
	logMu  sync.Mutex
	db     *sqlDB
	cfgErr error
	logF   *os.File
	calls  atomic.Int64
}

func (s *server) handleCall(msg rpc) rpc {
	fail := func(m string) rpc { return rpc{ID: msg.ID, Error: &rpcErr{Message: m}} }

	var p callParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		return fail("bad params: " + err.Error())
	}
	if s.cfgErr != nil {
		return fail(s.cfgErr.Error())
	}

	sqlText, args, err := p.sqlAndArgs()
	if err != nil {
		return fail("sql-postgres: " + err.Error())
	}

	ctx := context.Background()
	n := s.calls.Add(1)
	start := time.Now()

	var res rpc
	switch p.Operation {
	case "query":
		rows, err := s.db.query(ctx, sqlText, args)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: rows}
		}
	case "queryRow":
		row, err := s.db.queryRow(ctx, sqlText, args)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: row}
		}
	case "queryValue":
		v, err := s.db.queryValue(ctx, sqlText, args)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: v}
		}
	case "exec":
		affected, err := s.db.exec(ctx, sqlText, args)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: affected}
		}
	case "execScript":
		affected, err := s.db.execScript(ctx, sqlText)
		if err != nil {
			res = fail(err.Error())
		} else {
			res = rpc{ID: msg.ID, Result: affected}
		}
	default:
		res = fail("sql-postgres: unknown operation " + p.Operation)
	}

	s.logEvent(map[string]any{
		"ev": "call", "seq": n, "decl": p.Declaration.Name, "op": p.Operation,
		"sql_head": head(sqlText), "nargs": len(args),
		"dur_us": time.Since(start).Microseconds(), "err": res.Error != nil,
	})
	return res
}

func (s *server) config(p callParams) {
	s.once.Do(func() {
		props := map[string]any{}
		for _, pr := range p.Declaration.Props {
			props[pr.Name] = pr.Value
		}
		gets := func(k string) string { v, _ := props[k].(string); return v }

		if lp := gets("log"); lp != "" {
			if f, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				s.logF = f
			}
		}

		readOnly := true
		if b, ok := props["read_only"].(bool); ok {
			readOnly = b
		}
		var stmtTimeout time.Duration
		if v := gets("statement_timeout"); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				s.cfgErr = fmt.Errorf("sql-postgres: bad `statement_timeout` %q: %v", v, err)
				s.logEvent(map[string]any{"ev": "init", "pid": os.Getpid(), "err": errStr(s.cfgErr)})
				return
			}
			stmtTimeout = d
		}

		cfg := sqlConfig{
			DSN:              gets("dsn"),
			Host:             gets("host"),
			Port:             portString(props["port"]),
			DBName:           gets("dbname"),
			User:             gets("user"),
			Password:         gets("password"),
			SSLMode:          gets("sslmode"),
			ReadOnly:         readOnly,
			MaxRows:          int(numProp(props["max_rows"])),
			MaxConns:         int32(numProp(props["max_conns"])),
			StatementTimeout: stmtTimeout,
		}
		s.db, s.cfgErr = newSQLDB(context.Background(), cfg)

		s.logEvent(map[string]any{
			"ev": "init", "pid": os.Getpid(), "target": dsnTarget(cfg),
			"read_only": readOnly, "err": errStr(s.cfgErr),
		})
	})
}

func (s *server) shutdown(via string) {
	if s.db != nil {
		s.db.close()
	}
	s.logEvent(map[string]any{"ev": "shutdown", "calls": s.calls.Load(), "via": via})
}

func (s *server) logEvent(m map[string]any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.logF == nil {
		return
	}
	m["t"] = time.Now().UnixNano()
	b, _ := json.Marshal(m)
	_, _ = s.logF.Write(append(b, '\n'))
}

func head(sql string) string {
	sql = strings.Join(strings.Fields(sql), " ")
	if len(sql) > 80 {
		return sql[:80]
	}
	return sql
}

func errStr(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

func numProp(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		var f float64
		_, _ = fmt.Sscanf(n, "%g", &f)
		return f
	}
	return 0
}

func portString(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case float64:
		return strconv.Itoa(int(n))
	case json.Number:
		return n.String()
	}
	return ""
}
