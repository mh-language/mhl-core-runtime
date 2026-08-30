package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/mh-language/mhl-core-runtime/internal/a2aserver"
	"github.com/mh-language/mhl-core-runtime/internal/mcpserver"
)

// runServe implements:
//
//	mhl serve mcp [dir]
//	mhl serve a2a [--addr host:port] [dir]
//
// Both expose every pipeline/workflow declared under dir (default ".") to
// another agent: `mcp` as MCP tools over newline-delimited JSON-RPC on
// stdin/stdout (the form an MCP client uses when it spawns this process),
// `a2a` as Agent2Agent skills over HTTP JSON-RPC. Diagnostics go to stderr;
// for `mcp`, stdout is a raw protocol stream.
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
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	// SIGINT/SIGTERM cancel the server context so an in-flight run stops and
	// Serve returns cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	defer loadSessionExtensions(os.Stderr)()

	return mcpserver.Serve(ctx, dir, os.Stdin, os.Stdout, os.Stderr)
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
