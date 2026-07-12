// gqlc is an sqlc analogue for graph query languages: it parses GQL
// graph schemas and openCypher queries and generates type-safe Go from
// them. The command layer lives in internal/cli; main holds the
// module's only os.Exit, so defers in the command layer always fire.
package main

import (
	"os"

	"github.com/areqag/gqlc/internal/cli"
)

func main() {
	os.Exit(cli.Main())
}
