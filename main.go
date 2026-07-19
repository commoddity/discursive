// Command discursive is the local OpenAI-compatible gateway CLI.
package main

import (
	"os"

	"discursive/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
