// Command mhl is the CLI entry point for the Meta-Harness Language runtime.
package main

import (
	"fmt"
	"os"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout); err != nil {
		// Scrub any resolved credential out of the failure message before
		// it reaches the terminal or a CI log.
		fmt.Fprintln(os.Stderr, "mhl:", auth.Redact(err.Error()))
		os.Exit(1)
	}
}
