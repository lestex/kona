// Package cni installs non-default CNIs. Cilium is installed from the host
// with the pinned Cilium CLI (Helm mode) against the cluster kubeconfig.
package cni

import (
	"strings"

	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/version"
)

// k3s keeps containerd's CNI directories under its data dir.
const (
	k3sCNIBin  = "/var/lib/rancher/k3s/data/cni"
	k3sCNIConf = "/var/lib/rancher/k3s/agent/etc/cni/net.d"
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
	if c.Distro == "k3s" {
		args = append(args, "--set", "cni.binPath="+k3sCNIBin, "--set", "cni.confPath="+k3sCNIConf)
	}
	return args
}

// CiliumStatusArgs waits until Cilium reports healthy.
func CiliumStatusArgs(kubeconfig, context, wait string) []string {
	return []string{"status", "--kubeconfig", kubeconfig, "--context", context, "--wait", "--wait-duration", wait}
}
