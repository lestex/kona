package k3s

import (
	"slices"
	"strings"
	"testing"

	"github.com/lestex/kona/internal/state"
)

func cluster(cps int) *state.Cluster {
	c := &state.Cluster{Name: "kona"}
	for i := 1; i <= cps; i++ {
		c.Nodes = append(c.Nodes, state.Node{Name: "kona-control-plane-" + string(rune('0'+i)),
			Role: state.RoleControlPlane, IP: "192.168.70.1" + string(rune('0'+i-1))})
	}
	c.Nodes = append(c.Nodes, state.Node{Name: "kona-worker-1", Role: state.RoleWorker, IP: "192.168.70.20"})
	return c
}

func args(t *testing.T, c *state.Cluster, name string) string {
	t.Helper()
	n, _ := c.Node(name)
	a, err := K3s{}.NodeArgs(c, n, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(a, " ")
}

func TestSingleServerUsesSQLite(t *testing.T) {
	c := cluster(1)
	a := args(t, c, "kona-control-plane-1")
	if !strings.HasPrefix(a, "server ") || strings.Contains(a, "--cluster-init") ||
		!strings.Contains(a, "--node-ip 192.168.70.10") || !strings.Contains(a, "--advertise-address 192.168.70.10") ||
		!strings.Contains(a, "--flannel-iface eth0") || strings.Contains(a, "tok") {
		t.Fatalf("server args: %s", a)
	}
}

func TestHAControlPlanes(t *testing.T) {
	c := cluster(3)
	if a := args(t, c, "kona-control-plane-1"); !strings.Contains(a, "--cluster-init") {
		t.Fatalf("first cp: %s", a)
	}
	a := args(t, c, "kona-control-plane-2")
	if !strings.Contains(a, "--server https://192.168.70.10:6443") || strings.Contains(a, "--cluster-init") {
		t.Fatalf("second cp: %s", a)
	}
}

func TestAgent(t *testing.T) {
	a := args(t, cluster(1), "kona-worker-1")
	if !strings.HasPrefix(a, "agent --server https://192.168.70.10:6443") || !strings.Contains(a, "--node-ip 192.168.70.20") {
		t.Fatalf("agent args: %s", a)
	}
}

func TestTokenOnlyInEnv(t *testing.T) {
	env := K3s{}.NodeEnv(nil, state.Node{}, "tok")
	if env["K3S_TOKEN"] != "tok" {
		t.Fatalf("env = %v", env)
	}
	if !slices.Equal(K3s{}.Kubectl("get", "nodes"), []string{"kubectl", "--kubeconfig", AdminKubeconfigPath, "get", "nodes"}) {
		t.Fatal("Kubectl args")
	}
}
