package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/a2aserver"
	"github.com/mh-language/mhl-core-runtime/internal/mcpserver"
)

// runServe implements:
//
//	mhl serve mcp [dir]
//	mhl serve mcp --http [--addr host:port] [--token t] [--state-dir path] [dir]
//	mhl serve a2a [--addr host:port] [dir]
//
// All expose every pipeline/workflow declared under dir (default ".") to
// another agent: `mcp` as MCP tools over newline-delimited JSON-RPC on
// stdin/stdout (the form an MCP client uses when it spawns this process) or,
// with --http, over the Streamable HTTP transport (one JSON-RPC message per
// POST /mcp); `a2a` as Agent2Agent skills over HTTP JSON-RPC. Diagnostics go
// to stderr; for stdio `mcp`, stdout is a raw protocol stream.
func runServe(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mhl serve <mcp|a2a> [args]")
	}
	switch args[0] {
	case "mcp":
		return runServeMCP(args[1:], out)
	case "a2a":
		return runServeA2A(args[1:], out)
	default:
		return fmt.Errorf("unknown serve target %q (want: mcp, a2a)", args[0])
	}
}

func runServeMCP(args []string, out io.Writer) error {
	var (
		httpMode   bool
		addr       = "127.0.0.1:8711"
		token      = os.Getenv("MHL_SERVE_TOKEN")
		stateDir   = os.Getenv("MHL_SERVE_STATE_DIR")
		principalH = os.Getenv("MHL_SERVE_PRINCIPAL_HEADER")
		dir        string
	)
	var drainTimeout time.Duration
	if v := os.Getenv("MHL_SERVE_DRAIN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("MHL_SERVE_DRAIN_TIMEOUT %q: %w", v, err)
		}
		drainTimeout = d
	}
	var maxRuns int
	if v := os.Getenv("MHL_SERVE_MAX_CONCURRENT_RUNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("MHL_SERVE_MAX_CONCURRENT_RUNS %q: want a non-negative integer", v)
		}
		maxRuns = n
	}
	var err error
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--http":
			httpMode = true
		case "--addr":
			if i+1 >= len(args) {
				return fmt.Errorf("--addr requires a host:port argument")
			}
			i++
			addr = args[i]
		case "--token":
			if i+1 >= len(args) {
				return fmt.Errorf("--token requires an argument")
			}
			i++
			token = args[i]
		case "--state-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("--state-dir requires a path argument")
			}
			i++
			stateDir = args[i]
		case "--drain-timeout":
			if i+1 >= len(args) {
				return fmt.Errorf("--drain-timeout requires a duration argument (e.g. 30s)")
			}
			i++
			drainTimeout, err = time.ParseDuration(args[i])
			if err != nil {
				return fmt.Errorf("--drain-timeout %q: %w", args[i], err)
			}
		case "--max-concurrent-runs":
			if i+1 >= len(args) {
				return fmt.Errorf("--max-concurrent-runs requires an integer argument")
			}
			i++
			maxRuns, err = strconv.Atoi(args[i])
			if err != nil || maxRuns < 0 {
				return fmt.Errorf("--max-concurrent-runs %q: want a non-negative integer", args[i])
			}
		case "--principal-header":
			if i+1 >= len(args) {
				return fmt.Errorf("--principal-header requires a header name (e.g. X-Mhl-Principal)")
			}
			i++
			principalH = args[i]
		default:
			if dir != "" {
				return fmt.Errorf("unexpected argument %q", args[i])
			}
			dir = args[i]
		}
	}
	if dir == "" {
		dir = "."
	}

	// SIGINT/SIGTERM cancel the server context so an in-flight run stops and
	// the server returns cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	defer loadSessionExtensions(os.Stderr)()

	if httpMode {
		if principalH != "" && token == "" {
			return fmt.Errorf("--principal-header needs --token / MHL_SERVE_TOKEN: without the shared gateway↔mhl secret the header is client-spoofable")
		}
		if token == "" && !isLoopbackAddr(addr) {
			fmt.Fprintf(os.Stderr, "mhl serve mcp --http: warning: binding %s with no --token/MHL_SERVE_TOKEN — the endpoint is unauthenticated\n", addr)
		}

		// A `extension store <Name>` declaration in the workflow directory
		// backs durable state (sessions + checkpoints) with that extension;
		// otherwise the on-disk .mhl/state tree is used.
		store, closeStore, err := discoverStoreExtension(dir, os.Stderr)
		if err != nil {
			return err
		}
		defer closeStore()
		if store == nil && stateDir == "" {
			fmt.Fprintf(os.Stderr, "mhl serve mcp --http: note: no --state-dir/MHL_SERVE_STATE_DIR and no `extension store` — async run state is per-process and lost on restart\n")
		}

		return mcpserver.ServeHTTP(ctx, mcpserver.HTTPConfig{
			Addr:              addr,
			Dir:               dir,
			Token:             token,
			StateDir:          stateDir,
			PrincipalHeader:   principalH,
			Store:             store,
			DrainTimeout:      drainTimeout,
			MaxConcurrentRuns: maxRuns,
		}, os.Stderr)
	}
	return mcpserver.Serve(ctx, dir, os.Stdin, os.Stdout, os.Stderr)
}

// isLoopbackAddr reports whether a host:port binds only the loopback
// interface.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func runServeA2A(args []string, out io.Writer) error {
	addr := "127.0.0.1:8710"
	var dir string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 >= len(args) {
				return fmt.Errorf("--addr requires a host:port argument")
			}
			i++
			addr = args[i]
		default:
			if dir != "" {
				return fmt.Errorf("unexpected argument %q", args[i])
			}
			dir = args[i]
		}
	}
	if dir == "" {
		dir = "."
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	defer loadSessionExtensions(out)()

	return a2aserver.Serve(ctx, addr, dir, out)
}
