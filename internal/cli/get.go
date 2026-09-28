package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lestex/kona/internal/cluster"
	"github.com/lestex/kona/internal/execx"
	"github.com/lestex/kona/internal/kubeconfig"
	"github.com/lestex/kona/internal/runtime/container"
	"github.com/spf13/cobra"
)

func newGetCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "get", Short: "Display resources (clusters, nodes, kubeconfig)"}
	cmd.AddCommand(newGetClustersCmd(g), newGetNodesCmd(g), newGetKubeconfigCmd(g))
	return cmd
}

func (g *globals) manager(cmd *cobra.Command) *cluster.Manager {
	return cluster.NewManager(g.store, container.New(execx.OS{}), cmd.ErrOrStderr())
}

func newGetClustersCmd(g *globals) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "List clusters, flagging orphaned and degraded ones",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			infos, err := g.manager(cmd).List(cmd.Context())
			if err != nil {
				return err
			}
			if done, err := printStructured(cmd.OutOrStdout(), output, infos); done {
				return err
			}
			if len(infos) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "No kona clusters.")
				return nil
			}
			w := newTable(cmd.OutOrStdout())
			fmt.Fprintln(w, "NAME\tPHASE\tSTATUS\tDISTRO\tNODES\tSUBNET\tISSUES")
			for _, i := range infos {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d/%d\t%s\t%s\n", i.Name, i.Phase, i.Status, dash(i.Distro),
					i.Running, i.Nodes, dash(i.Subnet), dash(strings.Join(i.Issues, "; ")))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			for _, i := range infos {
				if i.Status == cluster.StatusOrphan {
					fmt.Fprintf(cmd.ErrOrStderr(), "! %q has VMs/volumes but no state; remove with: kona delete cluster %s\n", i.Name, i.Name)
				}
			}
			return nil
		},
	}
	addOutputFlag(cmd, &output)
	return cmd
}

func newGetNodesCmd(g *globals) *cobra.Command {
	var name, output string
	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "List a cluster's nodes with VM state, readiness and clock skew",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			nodes, err := g.manager(cmd).Nodes(cmd.Context(), name)
			if err != nil {
				return err
			}
			if done, err := printStructured(cmd.OutOrStdout(), output, nodes); done {
				return err
			}
			w := newTable(cmd.OutOrStdout())
			fmt.Fprintln(w, "NAME\tROLE\tKIND\tIP\tVM\tREADY\tVERSION\tCLOCK SKEW")
			for _, n := range nodes {
				skew := "-"
				if n.SkewSeconds != nil {
					skew = fmt.Sprintf("%+.2fs", *n.SkewSeconds)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", n.Name, n.Role, n.Kind, n.IP, n.VM, n.Ready, dash(n.Version), skew)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&name, "name", DefaultClusterName, "cluster name")
	addOutputFlag(cmd, &output)
	return cmd
}

func newGetKubeconfigCmd(g *globals) *cobra.Command {
	var name string
	var merge bool
	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Print the cluster kubeconfig; --merge adds context kona-<name> to ~/.kube/config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := g.store.Load(name)
			if err != nil {
				return err
			}
			m := g.manager(cmd)
			raw, err := m.Kubeconfig(cmd.Context(), c)
			if err != nil {
				return err
			}
			path, err := kubeconfig.Path(name)
			if err != nil {
				return err
			}
			if err := kubeconfig.Write(path, raw); err != nil {
				return err
			}
			if !merge {
				_, err = cmd.OutOrStdout().Write(raw)
				return err
			}
			dst := os.Getenv("KUBECONFIG")
			if dst == "" || strings.Contains(dst, string(os.PathListSeparator)) {
				dst = filepath.Join(filepath.Dir(path), "config")
			}
			if err := kubeconfig.Merge(dst, raw); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Merged context %s into %s (current context unchanged)\n",
				kubeconfig.ContextName(name), dst)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", DefaultClusterName, "cluster name")
	cmd.Flags().BoolVar(&merge, "merge", false, "merge into ~/.kube/config (or $KUBECONFIG) instead of printing")
	return cmd
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
