// Package cni installs non-default CNIs. Cilium is installed from the host
// with the pinned Cilium CLI (Helm mode) against the cluster kubeconfig.
package cni

import (
	"strings"

	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/version"
)

// CiliumVersion is the pinned Cilium release without the leading "v".
func CiliumVersion() string { return strings.TrimPrefix(version.Get("CILIUM_VERSION"), "v") }

// CiliumInstallArgs returns `cilium install` arguments for c.
//
// Datapath: kube-proxy replacement with VXLAN tunneling (docs/networking.md:
// nodes are L2-adjacent per vmnet network, and CPU<->GPU traffic crosses
// WireGuard, so a tunnel keeps pod routing independent of the underlay).
// The API server is addressed directly (k8sServiceHost) because there is
// no kube-proxy to program the kubernetes Service.
//
// CNI paths stay at Cilium's defaults for both distros: with
// --flannel-backend=none k3s leaves containerd on /etc/cni/net.d and
// /opt/cni/bin (its own data-dir paths apply only in flannel mode).
func CiliumInstallArgs(c *state.Cluster, kubeconfig, context string) []string {
	cp := c.ControlPlanes()[0]
	args := []string{"install",
		"--kubeconfig", kubeconfig, "--context", context,
		"--version", CiliumVersion(),
		"--set", "kubeProxyReplacement=true",
		"--set", "k8sServiceHost=" + cp.IP,
		"--set", "k8sServicePort=6443",
		"--set", "routingMode=tunnel",
		"--set", "tunnelProtocol=vxlan",
		"--set", "ipam.mode=kubernetes",
		"--set", "operator.replicas=1",
		"--set", "hubble.enabled=true",
		"--set", "hubble.relay.enabled=true",
	}
	return args
}

// CiliumStatusArgs waits until Cilium reports healthy.
func CiliumStatusArgs(kubeconfig, context, wait string) []string {
	return []string{"status", "--kubeconfig", kubeconfig, "--context", context, "--wait", "--wait-duration", wait}
}
