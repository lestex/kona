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

func TestCiliumMTUWithGPUNodes(t *testing.T) {
	c := &state.Cluster{Name: "kona", Nodes: []state.Node{
		{Name: "cp", Role: state.RoleControlPlane, IP: "192.168.70.10"}}}
	if a := strings.Join(CiliumInstallArgs(c, "/kc", "x"), " "); strings.Contains(strings.ToLower(a), "mtu=") {
		t.Fatalf("CPU-only cluster must keep auto MTU: %s", a)
	}
	c.Nodes = append(c.Nodes, state.Node{Name: "g", Role: state.RoleGPUWorker})
	// Upper-case MTU is the chart key (values.yaml: "MTU: 0").
	if a := strings.Join(CiliumInstallArgs(c, "/kc", "x"), " "); !strings.Contains(a, "--set MTU=1370") {
		t.Fatalf("GPU cluster needs --set MTU=1370: %s", a)
	}
}
