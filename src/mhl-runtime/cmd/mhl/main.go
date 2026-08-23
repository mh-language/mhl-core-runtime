// Command mhl is the CLI entry point for the Meta-Harness Language runtime.
package main

import (
	"fmt"
	"os"

	"github.com/yanjustino/mhl-runtime/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mhl:", err)
		os.Exit(1)
	}
}
