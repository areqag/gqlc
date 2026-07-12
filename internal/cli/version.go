package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/areqag/gqlc/internal/version"
)

// newVersionCmd prints the bare version plus one LF — `dev\n` unless a
// release build overrode internal/version.Version via -ldflags -X.
// Bare, not "gqlc <version>": $(gqlc version) interpolates into
// scripts without field-splitting (spec §2.2).
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the gqlc version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// RunE, not Run: errcheck (check-blank) refuses a discarded
			// Fprintln error, and a failed stdout write should exit 1.
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version.Version)
			return err
		},
	}
}
