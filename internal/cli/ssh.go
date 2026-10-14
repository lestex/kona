package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/lestex/kona/internal/state"
	"github.com/spf13/cobra"
)

func newSSHCmd(g *globals) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "ssh NODE [-- COMMAND...]",
		Short: "Open a shell (or run a command) on a node",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := g.store.Load(name)
			if err != nil {
				return err
			}
			n, ok := c.Node(args[0])
			if !ok {
				return fmt.Errorf("cluster %q has no node %q (see: kona get nodes --name %s)", name, args[0], name)
			}
			if n.Kind != state.KindContainer {
				return fmt.Errorf("ssh to %s nodes is not implemented yet", n.Kind)
			}
			command := args[1:]
			execArgs := []string{"exec", "-i"}
			if len(command) == 0 {
				command = []string{"/bin/bash", "-l"}
				execArgs = append(execArgs, "-t")
			}
			execArgs = append(append(execArgs, n.Name), command...)
			x := exec.CommandContext(cmd.Context(), "container", execArgs...)
			x.Stdin, x.Stdout, x.Stderr = os.Stdin, os.Stdout, os.Stderr
			return x.Run()
		},
	}
	cmd.Flags().StringVar(&name, "name", DefaultClusterName, "cluster name")
	return cmd
}
