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

// Secrets are a cluster's join credentials, persisted 0600 by kona.
type Secrets map[string]string

// NodeSpec is what a distro needs from the node VM.
type NodeSpec struct {
	// Args are passed to the node image entrypoint.
	Args []string
	// Env is extra environment for the node.
	Env map[string]string
	// UseInit runs `container`'s init as PID 1 (false: the image runs
	// systemd as PID 1).
	UseInit bool
	// Tmpfs paths to mount (systemd wants /run and /tmp).
	Tmpfs []string
	// DataDir is where control planes mount their block volume (datastore).
	DataDir string
}

// Distro bootstraps one Kubernetes distribution.
type Distro interface {
	Name() string
	// DefaultImage is the node image for this distro at the pinned version.
	DefaultImage() string
	// NewSecrets generates fresh join credentials.
	NewSecrets() (Secrets, error)
	// NodeSpec describes the VM for node n.
	NodeSpec(c *state.Cluster, n state.Node, s Secrets) (NodeSpec, error)
	// Bootstrap runs after node n's VM has started (init or join). It must
	// be idempotent: create resumes call it again on running nodes.
	Bootstrap(ctx context.Context, x Execer, c *state.Cluster, n state.Node, s Secrets) error
	// AdminKubeconfig reads the admin kubeconfig from the first control plane.
	AdminKubeconfig(ctx context.Context, x Execer, c *state.Cluster) ([]byte, error)
	// Kubectl returns the command that runs kubectl as admin on a control plane.
	Kubectl(args ...string) []string
}
