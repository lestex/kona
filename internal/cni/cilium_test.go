package cni

import (
	"strings"
	"testing"

	"github.com/lestex/kona/internal/state"
)

func TestCiliumInstallArgs(t *testing.T) {
	c := &state.Cluster{Name: "kona", Distro: "k3s", Nodes: []state.Node{
		{Name: "kona-control-plane-1", Role: state.RoleControlPlane, IP: "192.168.70.10"}}}
	a := strings.Join(CiliumInstallArgs(c, "/kc", "kona-kona"), " ")
	for _, want := range []string{"install --kubeconfig /kc --context kona-kona", "kubeProxyReplacement=true",
		"k8sServiceHost=192.168.70.10", "routingMode=tunnel", "tunnelProtocol=vxlan", "hubble.relay.enabled=true",
		"cni.binPath=/var/lib/rancher/k3s/data/cni", "cni.confPath=/var/lib/rancher/k3s/agent/etc/cni/net.d"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in %s", want, a)
		}
	}
	c.Distro = "kubeadm"
	if a := strings.Join(CiliumInstallArgs(c, "/kc", "x"), " "); strings.Contains(a, "cni.binPath") {
		t.Errorf("kubeadm must use default CNI paths: %s", a)
	}
}
