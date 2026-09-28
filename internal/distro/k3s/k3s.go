// Package k3s bootstraps clusters with k3s (the default distro: one static
// binary, fast boot, small footprint; see docs/distros.md).
package k3s

import (
	"context"
	"fmt"

	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/state"
)

// AdminKubeconfigPath is where k3s writes the admin kubeconfig.
const AdminKubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

// K3s implements distro.Distro.
type K3s struct{}

var _ distro.Distro = K3s{}

// Name implements distro.Distro.
func (K3s) Name() string { return "k3s" }

// NodeArgs implements distro.Distro.
func (K3s) NodeArgs(c *state.Cluster, n state.Node, _ string) ([]string, error) {
	cps := c.ControlPlanes()
	if len(cps) == 0 {
		return nil, fmt.Errorf("cluster %s has no control plane", c.Name)
	}
	first := cps[0]
	join := "https://" + first.IP + ":6443"
	common := []string{
		"--node-ip", n.IP,
		"--flannel-iface", "eth0",
		"--node-label", "kona.dev/cluster=" + c.Name,
		"--node-label", "kona.dev/role=" + n.Role,
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
		if len(cps) > 1 {
			// Embedded etcd for HA; its data dir is under server/db on the
			// block volume.
			if n.Name == first.Name {
				args = append(args, "--cluster-init")
			} else {
				args = append(args, "--server", join)
			}
		}
		// Single server: kine/SQLite at server/db/state.db, also on the volume.
		return append(args, common...), nil
	case state.RoleWorker, state.RoleGPUWorker:
		return append([]string{"agent", "--server", join}, common...), nil
	default:
		return nil, fmt.Errorf("unknown role %q", n.Role)
	}
}

// NodeEnv implements distro.Distro. The token travels as K3S_TOKEN so it
// never shows up in k3s' own process arguments.
func (K3s) NodeEnv(_ *state.Cluster, _ state.Node, token string) map[string]string {
	return map[string]string{"K3S_TOKEN": token}
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
