package cli

import (
	"time"

	"github.com/spf13/cobra"
)

func newStartCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "start", Short: "Start a resource (cluster, node)"}
	var wait time.Duration
	cluster := &cobra.Command{
		Use:   "cluster [NAME]",
		Short: "Start a cluster's stopped nodes (after a host reboot)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := DefaultClusterName
			if len(args) == 1 {
				name = args[0]
			}
			return g.manager(cmd).Start(cmd.Context(), name, wait)
		},
	}
	cluster.Flags().DurationVar(&wait, "wait", 5*time.Minute, "how long to wait for all nodes to be Ready")
	var name string
	node := &cobra.Command{
		Use:   "node NODE...",
		Short: "Start stopped nodes and wait for them to be Ready",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.manager(cmd).StartNodes(cmd.Context(), name, args, wait)
		},
	}
	node.Flags().StringVar(&name, "name", DefaultClusterName, "cluster name")
	node.Flags().DurationVar(&wait, "wait", 5*time.Minute, "how long to wait for the nodes to be Ready")
	cmd.AddCommand(cluster, node)
	return cmd
}
