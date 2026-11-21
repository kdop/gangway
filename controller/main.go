// The controller of Gangway: the API for the AI and the door for the
// cli-tools.
package main

import (
	"os"

	"github.com/kdop/gangway/controller/internal/start"
)

func main() {
	os.Exit(start.Run(os.Args[1:], os.Stderr))
}
