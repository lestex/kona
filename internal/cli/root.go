// Package cli wires the kona command tree.
package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lestex/kona/internal/cluster"
	"github.com/lestex/kona/internal/execx"
	"github.com/lestex/kona/internal/runtime/container"
	"github.com/lestex/kona/internal/runtime/krunkit"

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

// manager returns a cluster Manager for real host tools. The krunkit
// runtime is attached only when krunkit and vmnet-helper are installed.
func (g *globals) manager(cmd *cobra.Command) *cluster.Manager {
	run := execx.OS{}
	m := cluster.NewManager(g.store, container.New(run), run, cmd.ErrOrStderr())
	m.GPU = gpuRuntime(cmd.Context(), run)
	return m
}

func gpuRuntime(ctx context.Context, run execx.Runner) *krunkit.Runtime {
	if _, err := exec.LookPath("krunkit"); err != nil {
		return nil
	}
	prefix, err := run.Run(ctx, "brew", "--prefix", "vmnet-helper")
	if err != nil {
		return nil
	}
	helper := filepath.Join(strings.TrimSpace(string(prefix)), "libexec", "vmnet-helper")
	if _, err := os.Stat(helper); err != nil {
		return nil
	}
	return krunkit.New(helper)
}

// defaultGPUImage is where kona expects the GPU node disk image.
func (g *globals) defaultGPUImage() (string, error) {
	if err := g.init(); err != nil {
		return "", err
	}
	return filepath.Join(g.store.Root, "images", cluster.DefaultGPUImageName()), nil
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
	root.AddCommand(newCreateCmd(g), newDeleteCmd(g), newGetCmd(g), newAddCmd(g), newStartCmd(g), newStopCmd(g), newVerifyCmd(g), newSSHCmd(g), newDoctorCmd(g), newVersionCmd(), newRelayCmd())
	return root
}
