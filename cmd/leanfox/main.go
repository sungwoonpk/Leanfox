// Command leanfox measures where an AI coding agent's tokens and money go.
package main

import (
	"os"

	"github.com/sungwoonpk/leanfox/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, cli.OSEnv()))
}
