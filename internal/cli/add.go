package cli

import (
	"time"

	"github.com/lestex/kona/internal/cluster"
	"github.com/spf13/cobra"
)

func newAddCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "add", Short: "Add a resource (node)"}
	cmd.AddCommand(newAddNodeCmd(g))
	return cmd
}

func newAddNodeCmd(g *globals) *cobra.Command {
	o := cluster.AddOptions{}
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Add a worker node to a cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return g.manager(cmd).AddNode(cmd.Context(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.Cluster, "name", DefaultClusterName, "cluster name")
	f.IntVar(&o.CPUs, "cpus", 2, "vCPUs")
	f.StringVar(&o.Memory, "memory", "2G", "fixed memory")
	f.DurationVar(&o.Wait, "wait", 5*time.Minute, "how long to wait for the node to be Ready")
	return cmd
}
