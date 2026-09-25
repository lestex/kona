// Package cli wires the kona command tree.
package cli

import (
	"github.com/spf13/cobra"
)

// NewRoot returns the root `kona` command.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "kona",
		Short:         "Kubernetes on Apple silicon, one lightweight VM per node",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd())
	return root
}
