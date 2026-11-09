package cli

import (
	"github.com/spf13/cobra"
)

func newStopCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "stop", Short: "Stop a resource (cluster, node)"}
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
	var name string
	node := &cobra.Command{
		Use:   "node NODE...",
		Short: "Stop nodes, keeping their VMs and disks (start again with `kona start node`)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.manager(cmd).StopNodes(cmd.Context(), name, args)
		},
	}
	node.Flags().StringVar(&name, "name", DefaultClusterName, "cluster name")
	cmd.AddCommand(cluster, node)
	return cmd
}
