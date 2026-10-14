package cli

import (
	"github.com/spf13/cobra"
)

func newStopCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "stop", Short: "Stop a resource (cluster)"}
	cluster := &cobra.Command{
		Use:   "cluster [NAME]",
		Short: "Stop a cluster's nodes, keeping VMs, disks and state (resume with `kona start cluster`)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := DefaultClusterName
			if len(args) == 1 {
				name = args[0]
			}
			return g.manager(cmd).Stop(cmd.Context(), name)
		},
	}
	cmd.AddCommand(cluster)
	return cmd
}
