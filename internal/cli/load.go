package cli

import "github.com/spf13/cobra"

func newLoadCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "load", Short: "Load a resource into a cluster (image)"}
	var name string
	image := &cobra.Command{
		Use:   "image IMAGE...",
		Short: "Push local `container` images into the cluster's kona-registry; pods pull them by their usual names",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.manager(cmd).LoadImages(cmd.Context(), name, args)
		},
	}
	image.Flags().StringVar(&name, "name", DefaultClusterName, "cluster name")
	cmd.AddCommand(image)
	return cmd
}
