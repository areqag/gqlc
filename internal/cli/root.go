// Package cli implements the gqlc command tree on cobra (ADR 0011).
// Main is the package's only exported symbol; cmd/gqlc passes its
// result straight to os.Exit.
package cli

import (
	"github.com/spf13/cobra"
)

// Main executes the root command and maps the outcome to a process
// exit code: 0 on success, 1 on any error (spec §2.3 — parse errors
// are the only error class at CLI-0).
func Main() int {
	if err := newRootCmd().Execute(); err != nil {
		return 1
	}
	return 0
}

// newRootCmd builds the command tree fresh per invocation — no
// package-level command variables, so parallel tests never share
// mutable cobra state.
//
// No Run: cobra's default prints help and exits 0 on bare invocation,
// and the root stays Run-less when generate lands (CLI-1) — gqlc is
// subcommand-shaped, like sqlc. No Version field either: the version
// subcommand is the one way to ask (spec §2.1).
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gqlc",
		Short: "Generate type-safe Go from a graph schema file and openCypher queries",
		Long: `gqlc generates type-safe Go from a graph schema file and a directory of
openCypher query files, the way sqlc does for SQL.

A project declares its generation pipeline in a config file (gqlc.yaml):
the schema path, the query directory, the output directory, and the
schema-language / query-language / driver axes.`,
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	root.AddCommand(newVersionCmd())
	return root
}
