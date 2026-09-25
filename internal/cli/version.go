package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/lestex/kona/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print kona version and the pinned component matrix",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "kona %s (commit %s)\n\n", version.Version, version.Commit)
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "COMPONENT\tVERSION")
			for _, c := range version.Matrix() {
				fmt.Fprintf(w, "%s\t%s\n", c.Name, c.Version)
			}
			return w.Flush()
		},
	}
}
