// Package cli wires the kona command tree.
package cli

import (
	"path/filepath"

	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/version"
	"github.com/spf13/cobra"
)

// DefaultClusterName is used when a command gets no cluster name.
const DefaultClusterName = "kona"

// globals is shared by every subcommand.
type globals struct {
	store *state.Store
}

func (g *globals) init() error {
	if g.store != nil {
		return nil
	}
	s, err := state.Default()
	if err != nil {
		return err
	}
	g.store = s
	return nil
}

// defaultKernel is where kona expects its guest kernel.
func (g *globals) defaultKernel() (string, error) {
	if err := g.init(); err != nil {
		return "", err
	}
	return filepath.Join(g.store.Root, "kernels", "vmlinux-"+version.Get("KERNEL_VERSION")+"-kona"), nil
}

// NewRoot returns the root `kona` command.
func NewRoot() *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:           "kona",
		Short:         "Kubernetes on Apple silicon, one lightweight VM per node",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return g.init()
		},
	}
	root.AddCommand(newCreateCmd(g), newDeleteCmd(g), newGetCmd(g), newSSHCmd(g), newDoctorCmd(g), newVersionCmd())
	return root
}
