// Package k3s bootstraps clusters with k3s (the default distro: one static
// binary, fast boot, small footprint; see docs/distros.md).
package k3s

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/version"
)

// AdminKubeconfigPath is where k3s writes the admin kubeconfig.
const AdminKubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

// DataDir holds kine/SQLite (single server) or embedded etcd (HA).
const DataDir = "/var/lib/rancher/k3s/server/db"

// SecretToken is the cluster join token key in Secrets.
const SecretToken = "token"

// K3s implements distro.Distro.
type K3s struct{}

var _ distro.Distro = K3s{}

// Name implements distro.Distro.
func (K3s) Name() string { return "k3s" }

// DefaultImage implements distro.Distro. OCI tags cannot contain '+'.
func (K3s) DefaultImage() string {
	return "ghcr.io/lestex/kona-node:" + strings.ReplaceAll(version.Get("K3S_VERSION"), "+", "-")
}

// NewSecrets implements distro.Distro.
func (K3s) NewSecrets() (distro.Secrets, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return distro.Secrets{SecretToken: hex.EncodeToString(b)}, nil
}

// NodeSpec implements distro.Distro. The token travels as K3S_TOKEN so it
// never shows up in k3s' own process arguments.
func (K3s) NodeSpec(c *state.Cluster, n state.Node, s distro.Secrets) (distro.NodeSpec, error) {
	args, err := nodeArgs(c, n)
	if err != nil {
		return distro.NodeSpec{}, err
	}
	return distro.NodeSpec{Args: args, Env: map[string]string{"K3S_TOKEN": s[SecretToken]},
		UseInit: true, DataDir: DataDir}, nil
}

func nodeArgs(c *state.Cluster, n state.Node) ([]string, error) {
	cps := c.ControlPlanes()
	if len(cps) == 0 {
		return nil, fmt.Errorf("cluster %s has no control plane", c.Name)
	}
	first := cps[0]
	join := "https://" + first.IP + ":6443"
	common := []string{
		"--node-ip", n.IP,
		"--node-label", "kona.dev/cluster=" + c.Name,
		"--node-label", "kona.dev/role=" + n.Role,
	}
	cilium := c.CNI == "cilium"
	if !cilium {
		common = append(common, "--flannel-iface", "eth0")
	}
	switch n.Role {
	case state.RoleControlPlane:
		args := []string{"server",
			"--advertise-address", n.IP,
			"--tls-san", n.IP, "--tls-san", n.Name,
			"--write-kubeconfig-mode", "0600",
			// traefik is a demo ingress controller; kona keeps the default
			// footprint small. ServiceLB stays: LoadBalancer services get
			// node IPs, which the macOS host can reach directly.
			"--disable", "traefik",
		}
		for _, cp := range cps {
			if cp.Name != n.Name {
				args = append(args, "--tls-san", cp.IP)
			}
		}
		if cilium {
			// Cilium replaces flannel, the network policy controller and
			// kube-proxy (kubeProxyReplacement=true); agents inherit these
			// settings from the server.
			args = append(args, "--flannel-backend", "none", "--disable-network-policy", "--disable-kube-proxy")
		}
		if len(cps) > 1 {
			// Embedded etcd for HA; its data dir is under DataDir.
			if n.Name == first.Name {
				args = append(args, "--cluster-init")
			} else {
				args = append(args, "--server", join)
			}
		}
		// Single server: kine/SQLite at DataDir/state.db.
		return append(args, common...), nil
	case state.RoleWorker, state.RoleGPUWorker:
		return append([]string{"agent", "--server", join}, common...), nil
	default:
		return nil, fmt.Errorf("unknown role %q", n.Role)
	}
}

// Bootstrap implements distro.Distro: k3s bootstraps itself from its args.
func (K3s) Bootstrap(context.Context, distro.Execer, *state.Cluster, state.Node, distro.Secrets) error {
	return nil
}

// AdminKubeconfig implements distro.Distro.
func (K3s) AdminKubeconfig(ctx context.Context, x distro.Execer, c *state.Cluster) ([]byte, error) {
	cps := c.ControlPlanes()
	if len(cps) == 0 {
		return nil, fmt.Errorf("cluster %s has no control plane", c.Name)
	}
	return x.Exec(ctx, cps[0].Name, "cat", AdminKubeconfigPath)
}

// Kubectl implements distro.Distro.
func (K3s) Kubectl(args ...string) []string {
	return append([]string{"kubectl", "--kubeconfig", AdminKubeconfigPath}, args...)
}
