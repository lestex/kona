package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/execx"
	"github.com/lestex/kona/internal/runtime/container"
	"github.com/lestex/kona/internal/runtime/krunkit"
	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/tools"
)

const adminKubeconfig = `apiVersion: v1
kind: Config
clusters: [{name: default, cluster: {server: "https://127.0.0.1:6443", certificate-authority-data: Q0E=}}]
users: [{name: default, user: {client-key-data: S0VZ}}]
contexts: [{name: default, context: {cluster: default, user: default}}]
current-context: default
`

// sim is an in-memory `container` CLI.
type sim struct {
	networks   map[string]string            // name -> subnet
	volumes    map[string]map[string]string // name -> labels
	containers map[string]map[string]string // name -> labels
	failRun    string                       // container name whose run fails
	notReady   bool
	cilium     bool     // cilium DaemonSet exists
	extraNodes []string // non-container nodes (GPU) reported by kubectl
}

func newSim() *sim {
	return &sim{networks: map[string]string{"default": "192.168.64.0/24"},
		volumes: map[string]map[string]string{}, containers: map[string]map[string]string{}}
}

func labelsFrom(args []string) map[string]string {
	l := map[string]string{}
	for i, a := range args {
		if a == "--label" && i+1 < len(args) {
			k, v, _ := strings.Cut(args[i+1], "=")
			l[k] = v
		}
	}
	return l
}

func (s *sim) runner() *execx.Fake {
	f := &execx.Fake{}
	f.Match = func(line string) (execx.FakeResponse, bool) {
		args := strings.Fields(line)
		if args[0] != "container" {
			return execx.FakeResponse{}, true
		}
		args = args[1:]
		ok := func(out string) (execx.FakeResponse, bool) { return execx.FakeResponse{Out: []byte(out)}, true }
		notFound := func() (execx.FakeResponse, bool) {
			return execx.FakeResponse{Err: &execx.Error{Stderr: "notFound", Err: errors.New("exit 1")}}, true
		}
		switch {
		case args[0] == "network" && args[1] == "ls":
			var ns []string
			for n, sub := range s.networks {
				ns = append(ns, fmt.Sprintf(`{"id":%q,"status":{"ipv4Subnet":%q}}`, n, sub))
			}
			return ok("[" + strings.Join(ns, ",") + "]")
		case args[0] == "network" && args[1] == "create":
			s.networks[args[len(args)-1]] = args[3]
			return ok("")
		case args[0] == "network" && args[1] == "rm":
			if _, found := s.networks[args[2]]; !found {
				return notFound()
			}
			delete(s.networks, args[2])
			return ok("")
		case args[0] == "volume" && args[1] == "ls":
			var vs []string
			for n, l := range s.volumes {
				b, _ := json.Marshal(l)
				vs = append(vs, fmt.Sprintf(`{"id":%q,"configuration":{"labels":%s}}`, n, b))
			}
			return ok("[" + strings.Join(vs, ",") + "]")
		case args[0] == "volume" && args[1] == "create":
			s.volumes[args[len(args)-1]] = labelsFrom(args)
			return ok("")
		case args[0] == "volume" && args[1] == "rm":
			delete(s.volumes, args[2])
			return ok("")
		case args[0] == "ls":
			var cs []string
			for n, l := range s.containers {
				b, _ := json.Marshal(l)
				cs = append(cs, fmt.Sprintf(`{"id":%q,"configuration":{"labels":%s},"status":{"state":"running"}}`, n, b))
			}
			return ok("[" + strings.Join(cs, ",") + "]")
		case args[0] == "run":
			name := args[slicesIndex(args, "--name")+1]
			if name == s.failRun {
				return execx.FakeResponse{Err: &execx.Error{Stderr: "boom", Err: errors.New("exit 1")}}, true
			}
			s.containers[name] = labelsFrom(args)
			return ok("")
		case args[0] == "rm":
			delete(s.containers, args[2])
			return ok("")
		case args[0] == "exec":
			cmd := strings.Join(args[2:], " ")
			switch {
			case strings.HasSuffix(cmd, "get --raw /readyz"):
				return ok("ok")
			case strings.HasSuffix(cmd, "get nodes -o json"):
				var items []string
				names := append([]string{}, s.extraNodes...)
				for n := range s.containers {
					names = append(names, n)
				}
				for _, n := range names {
					st := "True"
					if s.notReady {
						st = "False"
					}
					items = append(items, fmt.Sprintf(`{"metadata":{"name":%q},"status":{"conditions":[{"type":"Ready","status":%q}]}}`, n, st))
				}
				return ok(`{"items":[` + strings.Join(items, ",") + `]}`)
			case strings.HasPrefix(cmd, "cat "):
				return ok(adminKubeconfig)
			case strings.HasSuffix(cmd, "get daemonset cilium"):
				if !s.cilium {
					return notFound()
				}
				return ok("")
			}
		}
		return ok("")
	}
	return f
}

func slicesIndex(a []string, v string) int {
	for i, x := range a {
		if x == v {
			return i
		}
	}
	return -1
}

func setup(t *testing.T) (*Manager, *sim, *bytes.Buffer, CreateOptions) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	kernel := filepath.Join(t.TempDir(), "vmlinux")
	os.WriteFile(kernel, []byte("k"), 0o644)
	s := newSim()
	var log bytes.Buffer
	now := time.Unix(0, 0)
	host := &execx.Fake{}
	cache := &tools.Cache{Dir: t.TempDir()}
	os.WriteFile(cache.Path(tools.CiliumCLI()), []byte("#!/bin/sh"), 0o755) // pre-cached CLI
	m := &Manager{
		Store: &state.Store{Root: t.TempDir()}, Container: container.New(s.runner()), Log: &log,
		Host: host, Tools: cache,
		HostPrefixes: func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")} },
		Now:          func() time.Time { return now },
		Sleep: func(context.Context, time.Duration) error {
			now = now.Add(pollInterval)
			return nil
		},
	}
	o := CreateOptions{Name: "kona", ControlPlanes: 1, Workers: 2, Distro: "k3s", Image: "img",
		Kernel: kernel, DNS: "8.8.8.8", CPUs: 2, Memory: "2G", Wait: time.Minute}
	return m, s, &log, o
}

func TestCreateHappyPath(t *testing.T) {
	m, s, log, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatalf("Create: %v\n%s", err, log)
	}
	c, err := m.Store.Load("kona")
	if err != nil {
		t.Fatal(err)
	}
	if c.Phase != state.PhaseReady || len(c.Nodes) != 3 || c.Network.Subnet != "192.168.70.0/24" {
		t.Fatalf("state: %+v", c)
	}
	if cp := c.ControlPlanes()[0]; cp.Name != "kona-control-plane-1" || cp.IP != "192.168.70.10" || cp.Volume != "kona-control-plane-1-data" {
		t.Fatalf("control plane: %+v", cp)
	}
	if _, ok := s.containers["kona-worker-2"]; !ok || s.networks["kona-kona"] != "192.168.70.0/24" {
		t.Fatalf("sim: %+v", s)
	}
	kc, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".kube", "kona-kona"))
	if err != nil || !strings.Contains(string(kc), "https://192.168.70.10:6443") {
		t.Fatalf("kubeconfig: %s %v", kc, err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); !os.IsNotExist(err) {
		t.Fatal("~/.kube/config must not be written without --merge")
	}
	if err := m.Create(context.Background(), o); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second create: %v", err)
	}
}

func TestCreateRollsBackOnFailure(t *testing.T) {
	m, s, _, o := setup(t)
	s.failRun = "kona-worker-2"
	if err := m.Create(context.Background(), o); err == nil {
		t.Fatal("expected failure")
	}
	if len(s.containers) != 0 || len(s.volumes) != 0 || s.networks["kona-kona"] != "" {
		t.Fatalf("orphans left behind: %+v", s)
	}
	if _, err := m.Store.Load("kona"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("state left behind: %v", err)
	}
}

func TestCreateRetainThenResume(t *testing.T) {
	m, s, _, o := setup(t)
	s.notReady = true
	o.Retain = true
	err := m.Create(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "retained") {
		t.Fatalf("expected retained failure, got %v", err)
	}
	c, _ := m.Store.Load("kona")
	if c.Phase != state.PhaseFailed || len(s.containers) != 3 {
		t.Fatalf("retained state: %+v containers=%d", c, len(s.containers))
	}
	s.notReady = false
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if c, _ := m.Store.Load("kona"); c.Phase != state.PhaseReady {
		t.Fatalf("after resume: %s", c.Phase)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	m, s, _, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.Delete(context.Background(), "kona"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}
	if len(s.containers)+len(s.volumes) != 0 {
		t.Fatalf("left: %+v", s)
	}
}

func TestValidate(t *testing.T) {
	_, _, _, o := setup(t)
	bad := []func(*CreateOptions){
		func(o *CreateOptions) { o.ControlPlanes = 2 },
		func(o *CreateOptions) { o.GPUWorkers = 1 },
		func(o *CreateOptions) { o.CNI = "calico" },
		func(o *CreateOptions) { o.Memory = "lots" },
		func(o *CreateOptions) { o.Kernel = "/nope" },
		func(o *CreateOptions) { o.Name = "Bad" },
		func(o *CreateOptions) { o.Distro = "k0s" },
	}
	for i, f := range bad {
		oo := o
		f(&oo)
		if err := validate(oo); err == nil {
			t.Errorf("case %d: validate accepted %+v", i, oo)
		}
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"2G": 2 << 30, "1536M": 1536 << 20, "2048": 2048 << 20, "2GiB": 2 << 30} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v", in, got, err)
		}
	}
}

func TestListFlagsOrphansAndDegraded(t *testing.T) {
	m, s, _, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	// A VM from a crashed create of another cluster, and a missing node here.
	s.containers["ghost-worker-1"] = map[string]string{container.LabelCluster: "ghost"}
	delete(s.containers, "kona-worker-2")
	infos, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("infos: %+v", infos)
	}
	ghost, kona := infos[0], infos[1]
	if ghost.Name != "ghost" || ghost.Status != StatusOrphan || ghost.Nodes != 1 {
		t.Fatalf("ghost: %+v", ghost)
	}
	if kona.Status != StatusDegraded || kona.Running != 2 || kona.Issues[0] != "kona-worker-2 missing" {
		t.Fatalf("kona: %+v", kona)
	}
	// delete --all path: Delete works from labels for orphans too.
	if err := m.Delete(context.Background(), "ghost"); err != nil || s.containers["ghost-worker-1"] != nil {
		t.Fatalf("delete orphan: %v", err)
	}
}

func TestNodesReportsReadiness(t *testing.T) {
	m, _, _, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	ns, err := m.Nodes(context.Background(), "kona")
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 3 || ns[0].Ready != "true" || ns[0].VM != "running" || ns[2].IP != "192.168.70.21" {
		t.Fatalf("nodes: %+v", ns)
	}
}

func TestCreateWithCilium(t *testing.T) {
	m, s, log, o := setup(t)
	o.CNI = "cilium"
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatalf("Create: %v\n%s", err, log)
	}
	host := m.Host.(*execx.Fake)
	if len(host.Calls) != 2 || !strings.Contains(host.Calls[0], " install ") || !strings.Contains(host.Calls[1], " status ") {
		t.Fatalf("host calls: %q", host.Calls)
	}
	if c, _ := m.Store.Load("kona"); c.CNI != "cilium" {
		t.Fatalf("CNI in state = %q", c.CNI)
	}
	// Resume/idempotency: an existing DaemonSet skips the install.
	s.cilium = true
	host.Calls = nil
	if err := m.installCilium(context.Background(), mustDistro(t, "k3s"), mustLoad(t, m, "kona")); err != nil || len(host.Calls) != 0 {
		t.Fatalf("second install: %v %q", err, host.Calls)
	}
}

func mustDistro(t *testing.T, name string) distro.Distro {
	d, err := DistroFor(name)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustLoad(t *testing.T, m *Manager, name string) *state.Cluster {
	c, err := m.Store.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const venusSummary = `Devices:
========
GPU0:
	apiVersion         = 1.2.0
	deviceName         = Virtio-GPU Venus (Apple M3 Pro)
	driverName         = venus
GPU1:
	deviceName         = llvmpipe (LLVM 22.1.0, 128 bits)
	driverName         = llvmpipe
`

func gpuSetup(t *testing.T, venus bool) (*Manager, *sim, CreateOptions) {
	t.Helper()
	m, s, _, o := setup(t)
	dir := t.TempDir()
	script := func(name, body string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755)
		return p
	}
	m.GPU = &krunkit.Runtime{Krunkit: script("krunkit", "sleep 30"),
		VmnetHelper:   script("vmnet-helper", `echo '{"vmnet_mtu":1500}'; exec sleep 30`),
		HelperTimeout: 5 * time.Second, StopGrace: 200 * time.Millisecond}
	m.PortFree = func(int) bool { return true }
	host := m.Host.(*execx.Fake)
	host.Match = func(line string) (execx.FakeResponse, bool) {
		if strings.HasPrefix(line, "ssh ") && strings.Contains(line, "vulkaninfo") {
			if venus {
				return execx.FakeResponse{Out: []byte(venusSummary)}, true
			}
			return execx.FakeResponse{Out: []byte("GPU0:\n\tdeviceName = llvmpipe\n\tdriverName = llvmpipe\n")}, true
		}
		return execx.FakeResponse{}, false
	}
	img := filepath.Join(dir, "gpu.raw")
	os.WriteFile(img, []byte("raw"), 0o644)
	o.GPUWorkers, o.GPUCPUs, o.GPUMemory, o.GPUImage = 1, 4, "4G", img
	s.extraNodes = []string{"kona-gpu-worker-1"}
	return m, s, o
}

func TestCreateWithGPUWorker(t *testing.T) {
	m, s, o := gpuSetup(t, true)
	t.Cleanup(func() { m.Delete(context.Background(), "kona") })
	var calls []string
	inner := m.Container.Runner.(*execx.Fake)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatalf("Create: %v\n%s", err, m.Log)
	}
	calls = inner.Calls
	c := mustLoad(t, m, "kona")
	g := c.GPUNodes()
	if len(g) != 1 || g[0].Name != "kona-gpu-worker-1" || g[0].Kind != state.KindKrunkit ||
		c.GPUNetwork.Subnet != "192.168.71.0/24" || g[0].IP != "192.168.71.10" {
		t.Fatalf("gpu plan: %+v %+v", c.GPUNetwork, g)
	}
	ports := map[int]bool{}
	for _, n := range c.CPUNodes() {
		if n.WGPort < 51820 || ports[n.WGPort] {
			t.Fatalf("bad/duplicate WG port on %s: %d", n.Name, n.WGPort)
		}
		ports[n.WGPort] = true
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, fmt.Sprintf("-p %d:51820/udp", c.ControlPlanes()[0].WGPort)) {
		t.Fatal("control plane does not publish its WireGuard port")
	}
	if !strings.Contains(joined, "exec kona-worker-2 /usr/local/bin/kona-wg") {
		t.Fatal("CPU nodes were not given the WireGuard config")
	}
	kn := m.krunkitNode(c, g[0])
	if !m.GPU.Running(kn) {
		t.Fatal("GPU VM not running")
	}
	if _, err := os.Stat(kn.Seed()); err != nil {
		t.Fatalf("seed ISO: %v", err)
	}
	if !strings.Contains(joined, "label node kona-gpu-worker-1 --overwrite kona.dev/gpu=venus") {
		t.Fatalf("Venus label missing:\n%s", joined)
	}
	if strings.Contains(joined, "taint node kona-gpu-worker-1") {
		t.Fatal("Venus node must stay tainted")
	}
	if err := m.Delete(context.Background(), "kona"); err != nil {
		t.Fatal(err)
	}
	if m.GPU.Running(kn) || len(s.containers) != 0 {
		t.Fatal("GPU VM or containers survived delete")
	}
}

func TestGPUWithoutVenusIsUntainted(t *testing.T) {
	m, _, o := gpuSetup(t, false)
	t.Cleanup(func() { m.Delete(context.Background(), "kona") })
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(m.Container.Runner.(*execx.Fake).Calls, "\n")
	if !strings.Contains(joined, "taint node kona-gpu-worker-1 kona.dev/gpu-") ||
		!strings.Contains(joined, "kona.dev/gpu=unavailable") {
		t.Fatalf("no untaint/label for a node without Venus:\n%s", joined)
	}
	if !strings.Contains(m.Log.(*bytes.Buffer).String(), "WARNING") {
		t.Fatal("no loud warning")
	}
}

func TestGPUSeed(t *testing.T) {
	c := &state.Cluster{Name: "kona", DNS: "8.8.8.8",
		GPUNetwork: state.Network{Subnet: "192.168.71.0/24", Gateway: "192.168.71.1", Prefix: 24}}
	n := state.Node{Name: "kona-gpu-worker-1", IP: "192.168.71.10", MAC: "52:54:00:aa:bb:cc"}
	seed := GPUSeed(c, n, "ssh-ed25519 AAAA kona-kona", "TOKEN", "[Interface]\n", "192.168.70.0/24\n", []string{"--server", "https://x:6443"})
	nc := seed["network-config"]
	for _, want := range []string{`macaddress: "52:54:00:aa:bb:cc"`, "addresses: [192.168.71.10/24]", "via: 192.168.71.1", "addresses: [8.8.8.8]"} {
		if !strings.Contains(nc, want) {
			t.Errorf("network-config missing %q:\n%s", want, nc)
		}
	}
	ud := seed["user-data"]
	if !strings.Contains(ud, "/etc/kona/k3s-agent.env, permissions: '0600'") || strings.Contains(ud, "TOKEN") {
		t.Errorf("token must be inside the 0600 env file (base64), not inline:\n%s", ud)
	}
}

func TestVenusDevice(t *testing.T) {
	if d := VenusDevice(venusSummary); d != "Virtio-GPU Venus (Apple M3 Pro)" {
		t.Fatalf("VenusDevice = %q", d)
	}
	if d := VenusDevice("GPU0:\n deviceName = llvmpipe\n driverName = llvmpipe\n"); d != "" {
		t.Fatalf("llvmpipe detected as Venus: %q", d)
	}
}
