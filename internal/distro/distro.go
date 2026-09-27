// Package distro abstracts how a Kubernetes distribution is bootstrapped on
// kona nodes.
package distro

import (
	"context"

	"github.com/lestex/kona/internal/state"
)

// Execer runs a command inside a node.
type Execer interface {
	Exec(ctx context.Context, node string, cmd ...string) ([]byte, error)
}

// Distro bootstraps one Kubernetes distribution.
type Distro interface {
	Name() string
	// NodeArgs returns the node image entrypoint arguments. token is the
	// cluster join secret.
	NodeArgs(c *state.Cluster, n state.Node, token string) ([]string, error)
	// NodeEnv returns extra environment for the node.
	NodeEnv(c *state.Cluster, n state.Node, token string) map[string]string
	// AdminKubeconfig reads the admin kubeconfig from the first control plane.
	AdminKubeconfig(ctx context.Context, x Execer, c *state.Cluster) ([]byte, error)
	// Kubectl returns the command that runs kubectl as admin on a control plane.
	Kubectl(args ...string) []string
}
