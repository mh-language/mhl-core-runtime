package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// A tiny, dependency-free Redis client: RESP2 request encoding and reply
// parsing, a small connection pool, and just the handful of commands this
// cache extension issues (GET/SET/DEL/EXISTS/INCR/INCRBY/EXPIRE/TTL, plus
// AUTH/SELECT/PING at connect). No pub/sub, no pipelining, no cluster.

const (
	defaultAddr     = "localhost:6379"
	defaultPoolSize = 8
	defaultDial     = 5 * time.Second
	defaultRead     = 3 * time.Second
)

type redisError string

func (e redisError) Error() string { return "redis: " + string(e) }

type clientConfig struct {
	Addr          string
	Username      string
	Password      string
	DB            int
	TLS           bool
	TLSSkipVerify bool
	PoolSize      int
	DialTimeout   time.Duration
	ReadTimeout   time.Duration
}

type client struct {
	cfg  clientConfig
	pool chan *conn
}

func newClient(ctx context.Context, cfg clientConfig) (*client, error) {
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = defaultPoolSize
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = defaultDial
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = defaultRead
	}
	c := &client{cfg: cfg, pool: make(chan *conn, cfg.PoolSize)}

	// Fail fast: one connection must handshake and PING.
	cn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := cn.do(ctx, cfg.ReadTimeout, "PING"); err != nil {
		cn.close()
		return nil, err
	}
	c.put(cn)
	return c, nil
}

func (c *client) close() {
	for {
		select {
		case cn := <-c.pool:
			cn.close()
		default:
			return
		}
	}
}

func (c *client) dial(ctx context.Context) (*conn, error) {
	d := net.Dialer{Timeout: c.cfg.DialTimeout}
	raw, err := d.DialContext(ctx, "tcp", c.cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("redis: dial %s: %w", c.cfg.Addr, err)
	}
	var nc net.Conn = raw
	if c.cfg.TLS {
		host, _, _ := net.SplitHostPort(c.cfg.Addr)
		tc := tls.Client(raw, &tls.Config{ServerName: host, InsecureSkipVerify: c.cfg.TLSSkipVerify})
		if err := tc.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, fmt.Errorf("redis: TLS handshake: %w", err)
		}
		nc = tc
	}
	cn := &conn{nc: nc, br: bufio.NewReader(nc)}

	if c.cfg.Password != "" {
		args := []string{"AUTH", c.cfg.Password}
		if c.cfg.Username != "" {
			args = []string{"AUTH", c.cfg.Username, c.cfg.Password}
		}
		if _, err := cn.do(ctx, c.cfg.ReadTimeout, args...); err != nil {
			cn.close()
			return nil, err
		}
	}
	if c.cfg.DB != 0 {
		if _, err := cn.do(ctx, c.cfg.ReadTimeout, "SELECT", strconv.Itoa(c.cfg.DB)); err != nil {
			cn.close()
			return nil, err
		}
	}
	return cn, nil
}

func (c *client) get(ctx context.Context) (*conn, error) {
	select {
	case cn := <-c.pool:
		return cn, nil
	default:
		return c.dial(ctx)
	}
}

func (c *client) put(cn *conn) {
	if cn == nil || cn.dead {
		if cn != nil {
			cn.close()
		}
		return
	}
	select {
	case c.pool <- cn:
	default:
		cn.close()
	}
}

// do runs one command, retrying once on a transport error with a fresh
// connection. A Redis-level error (`-ERR …`) is returned as-is, never retried.
func (c *client) do(ctx context.Context, args ...string) (any, error) {
	cn, err := c.get(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := cn.do(ctx, c.cfg.ReadTimeout, args...)
	if err == nil {
		c.put(cn)
		return reply, nil
	}
	var rerr redisError
	if errors.As(err, &rerr) {
		c.put(cn)
		return nil, err
	}
	// transport error — drop this conn, try one fresh one.
	cn.close()
	cn2, derr := c.dial(ctx)
	if derr != nil {
		return nil, derr
	}
	reply, err = cn2.do(ctx, c.cfg.ReadTimeout, args...)
	if err != nil {
		cn2.close()
		return nil, err
	}
	c.put(cn2)
	return reply, nil
}

type conn struct {
	nc   net.Conn
	br   *bufio.Reader
	dead bool
}

func (cn *conn) close() {
	cn.dead = true
	_ = cn.nc.Close()
}

func (cn *conn) do(ctx context.Context, readTimeout time.Duration, args ...string) (any, error) {
	if d, ok := ctx.Deadline(); ok {
		_ = cn.nc.SetDeadline(d)
	} else {
		_ = cn.nc.SetWriteDeadline(time.Now().Add(readTimeout))
	}
	if _, err := cn.nc.Write(encodeCommand(args)); err != nil {
		cn.dead = true
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		_ = cn.nc.SetReadDeadline(time.Now().Add(readTimeout))
	}
	reply, err := readReply(cn.br)
	if err != nil {
		var rerr redisError
		if !errors.As(err, &rerr) {
			cn.dead = true
		}
		return nil, err
	}
	return reply, nil
}

func encodeCommand(args []string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	return b.Bytes()
}

// readReply parses one RESP2 value: +simple, -error, :int, $bulk (nil = Go
// nil), *array (nil = Go nil).
func readReply(r *bufio.Reader) (any, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, redisError(line)
	case ':':
		return strconv.ParseInt(line, 10, 64)
	case '$':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("redis: bad bulk length %q", line)
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return buf[:n], nil
	case '*':
		n, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("redis: bad array length %q", line)
		}
		if n < 0 {
			return nil, nil
		}
		arr := make([]any, n)
		for i := 0; i < n; i++ {
			if arr[i], err = readReply(r); err != nil {
				return nil, err
			}
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("redis: unknown reply type %q", prefix)
	}
}

func readLine(r *bufio.Reader) (string, error) {
	s, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	s = strings.TrimRight(s, "\r\n")
	return s, nil
}
