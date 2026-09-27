package cli

import (
	"errors"
	"fmt"

	"github.com/lestex/kona/internal/cluster"
	"github.com/lestex/kona/internal/execx"
	"github.com/lestex/kona/internal/runtime/container"
	"github.com/spf13/cobra"
)

func newDeleteCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "delete", Short: "Delete a resource (cluster)"}
	cmd.AddCommand(newDeleteClusterCmd(g))
	return cmd
}

func newDeleteClusterCmd(g *globals) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "cluster [NAME]",
		Short: "Delete a cluster: VMs, volumes, network, kubeconfig and state (idempotent)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			m := cluster.NewManager(g.store, container.New(execx.OS{}), execx.OS{}, cmd.ErrOrStderr())
			var names []string
			switch {
			case all && len(args) > 0:
				return errors.New("pass a cluster name or --all, not both")
			case all:
				infos, err := m.List(ctx)
				if err != nil {
					return err
				}
				for _, i := range infos {
					names = append(names, i.Name)
				}
			case len(args) == 1:
				names = []string{args[0]}
			default:
				names = []string{DefaultClusterName}
			}
			var errs []error
			for _, n := range names {
				if err := m.Delete(ctx, n); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", n, err))
					continue
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "✓ Deleted cluster %q\n", n)
			}
			return errors.Join(errs...)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "delete every kona cluster, including orphans without state")
	return cmd
}
