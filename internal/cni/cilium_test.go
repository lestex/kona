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
		"k8sServiceHost=192.168.70.10", "routingMode=tunnel", "tunnelProtocol=vxlan", "hubble.relay.enabled=true"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in %s", want, a)
		}
	}
	if strings.Contains(a, "cni.binPath") || strings.Contains(a, "cni.confPath") {
		t.Errorf("k3s in cilium mode uses containerd's default CNI paths: %s", a)
	}
}
