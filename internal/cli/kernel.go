package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/lestex/kona/internal/kernel"
	"github.com/spf13/cobra"
)

func newKernelCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kernel",
		Short: "Manage the kona guest kernel",
	}
	cmd.AddCommand(newKernelInstallCmd(g))
	return cmd
}

func newKernelInstallCmd(g *globals) *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the pinned guest kernel, from its GitHub release or a local build",
		Long: "Downloads the guest kernel this kona pins (versions.env) from its GitHub\n" +
			"release, or copies a local `make -C kernel` build with --from, and checks\n" +
			"its sha256 against the pin before installing it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := g.defaultKernel()
			if err != nil {
				return err
			}
			pin := kernel.Pin()
			if pin == "" {
				return errors.New("this kona build has no kernel pin (build it with `make build`)")
			}
			out := cmd.OutOrStdout()
			if kernel.Installed(path, pin) {
				fmt.Fprintf(out, "✓ %s is already installed (%s)\n", kernel.Name(), path)
				return nil
			}
			var src io.ReadCloser
			if from != "" {
				if src, err = os.Open(from); err != nil {
					return err
				}
			} else {
				url := kernel.URL(pin)
				fmt.Fprintf(out, "• Downloading %s\n", url)
				if src, err = kernel.Download(cmd.Context(), http.DefaultClient, url); err != nil {
					return err
				}
			}
			defer src.Close()
			if err := kernel.Install(path, src, pin); err != nil {
				return err
			}
			fmt.Fprintf(out, "✓ Installed %s at %s\n", kernel.Name(), path)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "install this local kernel file (e.g. kernel/out/vmlinux-<ver>-kona) instead of downloading")
	return cmd
}
