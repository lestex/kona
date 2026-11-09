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
		Short: "Add a worker node to a cluster (--gpu: a krunkit GPU worker with Vulkan via Venus)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if !cmd.Flags().Changed("cpus") && o.GPU {
				o.CPUs = 4
			}
			if !cmd.Flags().Changed("memory") && o.GPU {
				o.Memory = "4G"
			}
			if o.GPU {
				if o.GPUImage == "" {
					img, err := g.defaultGPUImage()
					if err != nil {
						return err
					}
					o.GPUImage = img
				}
			}
			return g.manager(cmd).AddNode(ctx, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.Cluster, "name", DefaultClusterName, "cluster name")
	f.BoolVar(&o.GPU, "gpu", false, "add a krunkit GPU worker instead of an Apple container worker")
	f.IntVar(&o.CPUs, "cpus", 2, "vCPUs (GPU default: 4)")
	f.StringVar(&o.Memory, "memory", "2G", "fixed memory (GPU default: 4G)")
	f.StringVar(&o.GPUImage, "gpu-image", "", "GPU node disk image (default: the installed kona-gpu-node image)")
	f.DurationVar(&o.Wait, "wait", 5*time.Minute, "how long to wait for the node to be Ready")
	return cmd
}
