// Package adapters contains local agent process adapters.
package adapters

import (
	"context"
	"github.com/mh-language/mhl-core-runtime/internal/features/tools"
)

// CLI runs a local agent command in an isolated process group.
type CLI struct{ Command tools.Cmd }

func (c CLI) Run(ctx context.Context, name string, args ...string) (tools.Result, error) {
	return c.Command.Exec(ctx, name, args...)
}
