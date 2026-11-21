// The gangway cli-tool: it runs on the host and holds the gate.
package main

import (
	"os"

	"github.com/kdop/gangway/cli/internal/start"
)

func main() {
	os.Exit(start.Run(os.Stdin, os.Stdout, os.Stderr))
}
