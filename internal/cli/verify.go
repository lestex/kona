package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newVerifyCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "verify", Short: "Check a resource's health without changing it (cluster)"}
	var output string
	var timeout time.Duration
	cluster := &cobra.Command{
		Use:   "cluster [NAME]",
		Short: "Check VMs, relays, API, nodes, system pods, DNS, clocks and GPUs; exit 1 on any failure",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := DefaultClusterName
			if len(args) == 1 {
				name = args[0]
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			r, err := g.manager(cmd).Verify(ctx, name)
			if err != nil {
				return err
			}
			if done, err := printStructured(cmd.OutOrStdout(), output, r); done {
				if err != nil {
					return err
				}
			} else {
				w := newTable(cmd.OutOrStdout())
				fmt.Fprintln(w, "STATUS\tCHECK\tDETAIL")
				for _, c := range r.Checks {
					fmt.Fprintf(w, "%s\t%s\t%s\n", strings.ToUpper(c.Status), c.ID, c.Detail)
				}
				if err := w.Flush(); err != nil {
					return err
				}
				for _, c := range r.Checks {
					if c.Hint != "" && c.Status != "pass" {
						fmt.Fprintf(cmd.ErrOrStderr(), "! %s: %s\n", c.ID, c.Hint)
					}
				}
			}
			if n := r.Failed(); n > 0 {
				cmd.SilenceUsage = true
				return fmt.Errorf("%d check(s) failed", n)
			}
			return nil
		},
	}
	addOutputFlag(cluster, &output)
	cluster.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "give up after this long")
	cmd.AddCommand(cluster)
	return cmd
}
