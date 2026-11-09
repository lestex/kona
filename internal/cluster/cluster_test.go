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
	cilium     bool // cilium DaemonSet exists
	// boot is each VM's current boot ID; the kubelet's report (reported)
	// catches up lag polls of `get nodes` after it changes.
	boot, reported map[string]string
	lag            int
	nodePolls      int
	stopped        map[string]bool // containers that are stopped
	// verify's view: node IPs, and switches that break one thing.
	ips                           map[string]string
	clock                         string // guest `date +%s.%N`
	crashPod, dsNotReady, apiDown bool
	unhealthy                     int // polls a DaemonSet stays not ready
}

// objects answers `kubectl get nodes,pods,daemonsets,deployments -A -o json`.
func (s *sim) objects(names []string) string {
	var items []string
	for _, n := range names {
		addr := "" // unknown unless the test recorded the node's IP
		if ip := s.ips[n]; ip != "" {
			addr = fmt.Sprintf(`{"type":"InternalIP","address":%q}`, ip)
		}
		items = append(items, fmt.Sprintf(`{"kind":"Node","metadata":{"name":%q},"status":{"conditions":[{"type":"Ready","status":"True"}],`+
			`"addresses":[%s],"nodeInfo":{"bootID":%q}}}`,
			n, addr, s.reportedBoot(n)))
	}
	pod := `{"kind":"Pod","metadata":{"name":"coredns-1","namespace":"kube-system"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`
	if s.crashPod {
		pod = `{"kind":"Pod","metadata":{"name":"cilium-x","namespace":"kube-system"},"status":{"phase":"Running",` +
			`"containerStatuses":[{"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}`
	}
	ready := len(names)
	if s.dsNotReady || s.unhealthy > 0 {
		ready--
	}
	s.unhealthy--
	items = append(items, pod,
		fmt.Sprintf(`{"kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system"},"status":{"desiredNumberScheduled":%d,"numberReady":%d}}`, len(names), ready),
		`{"kind":"Deployment","metadata":{"name":"coredns","namespace":"kube-system"},"status":{"readyReplicas":1}}`)
	return `{"items":[` + strings.Join(items, ",") + `]}`
}

func (s *sim) bootOf(n string) string {
	if s.boot[n] == "" {
		s.boot[n] = "boot-" + n
	}
	return s.boot[n]
}

func (s *sim) reportedBoot(n string) string {
	if s.reported[n] == "" {
		s.reported[n] = s.bootOf(n)
	}
	if s.reported[n] != s.bootOf(n) {
		if s.lag <= 0 {
			s.reported[n] = s.bootOf(n)
		}
	}
	return s.reported[n]
}

func newSim() *sim {
	return &sim{networks: map[string]string{"default": "192.168.64.0/24"},
		volumes: map[string]map[string]string{}, containers: map[string]map[string]string{},
		boot: map[string]string{}, reported: map[string]string{}, stopped: map[string]bool{},
		ips: map[string]string{}}
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
				st := "running"
				if s.stopped[n] {
					st = "stopped"
				}
				cs = append(cs, fmt.Sprintf(`{"id":%q,"configuration":{"labels":%s},"status":{"state":%q}}`, n, b, st))
			}
			return ok("[" + strings.Join(cs, ",") + "]")
		case args[0] == "run":
			name := args[slicesIndex(args, "--name")+1]
			if name == s.failRun {
				return execx.FakeResponse{Err: &execx.Error{Stderr: "boom", Err: errors.New("exit 1")}}, true
			}
			s.containers[name] = labelsFrom(args)
			return ok("")
		case args[0] == "stop":
			s.stopped[args[len(args)-1]] = true
			return ok("")
		case args[0] == "start":
			if s.stopped[args[1]] {
				delete(s.stopped, args[1])
				s.boot[args[1]] = s.bootOf(args[1]) + "+"
			}
			return ok("")
		case args[0] == "rm":
			delete(s.containers, args[2])
			return ok("")
		case args[0] == "exec":
			cmd := strings.Join(args[2:], " ")
			switch {
			case strings.HasSuffix(cmd, "get --raw /readyz"):
				if s.apiDown {
					return execx.FakeResponse{Err: &execx.Error{Stderr: "connection refused", Err: errors.New("exit 1")}}, true
				}
				return ok("ok")
			case strings.HasSuffix(cmd, "get nodes,pods,daemonsets,deployments -A -o json"):
				var names []string
				for n := range s.containers {
					names = append(names, n)
				}
				return ok(s.objects(names))
			case cmd == "date +%s.%N":
				return ok(s.clock)
			case strings.HasSuffix(cmd, "get nodes -o json"):
				var items, names []string
				for n := range s.containers {
					names = append(names, n)
				}
				s.nodePolls++
				for _, n := range names {
					st := "True"
					if s.notReady {
						st = "False"
					}
					items = append(items, fmt.Sprintf(`{"metadata":{"name":%q},"status":{"conditions":[{"type":"Ready","status":%q}],"nodeInfo":{"bootID":%q}}}`,
						n, st, s.reportedBoot(n)))
				}
				s.lag--
				return ok(`{"items":[` + strings.Join(items, ",") + `]}`)
			case cmd == "cat /proc/sys/kernel/random/boot_id":
				return ok(s.bootOf(args[1]) + "\n")
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
		HostDial:     func(string) error { return nil },
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
		func(o *CreateOptions) { o.Workers = -1 },
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

// An older kona wrote krunkit GPU nodes into state; this one refuses to
// manage such a cluster but still deletes it.
func TestGPUClusterFromOlderKona(t *testing.T) {
	m, s, log, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	c := mustLoad(t, m, "kona")
	c.Nodes = append(c.Nodes, state.Node{Name: "kona-gpu-worker-1", Role: "gpu-worker", Kind: "krunkit", IP: "192.168.71.10"})
	if err := m.Store.Save(c); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"start":  m.Start(context.Background(), "kona", time.Minute),
		"stop":   m.Stop(context.Background(), "kona"),
		"add":    m.AddNode(context.Background(), AddOptions{Cluster: "kona", CPUs: 2, Memory: "2G", Wait: time.Minute}),
		"verify": func() error { _, err := m.Verify(context.Background(), "kona"); return err }(),
	} {
		if err == nil || !strings.Contains(err.Error(), "gpu branch") {
			t.Errorf("%s: want unsupported-cluster error, got %v", name, err)
		}
	}
	if err := m.Delete(context.Background(), "kona"); err != nil || len(s.containers) != 0 {
		t.Fatalf("delete: %v, left %v", err, s.containers)
	}
	if !strings.Contains(log.String(), "kona-gpu-worker-1 is a krunkit node") {
		t.Fatalf("delete did not warn about the GPU VM:\n%s", log)
	}
}

func TestStopThenStartCluster(t *testing.T) {
	m, s, _, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	c := mustLoad(t, m, "kona")
	inner := m.Container.Runner.(*execx.Fake)
	inner.Calls = nil
	if err := m.Stop(context.Background(), "kona"); err != nil {
		t.Fatal(err)
	}
	if len(s.stopped) != len(c.Nodes) {
		t.Fatalf("not everything stopped: %v", s.stopped)
	}
	var order []string
	for _, call := range inner.Calls {
		if strings.HasPrefix(call, "container stop ") {
			order = append(order, call[strings.LastIndex(call, " ")+1:])
		}
	}
	if len(order) == 0 || order[len(order)-1] != "kona-control-plane-1" {
		t.Fatalf("control plane must stop last: %v", order)
	}
	infos, err := m.List(context.Background())
	if err != nil || len(infos) != 1 || infos[0].Status != StatusStopped || len(infos[0].Issues) != 0 {
		t.Fatalf("list after stop: %+v %v", infos, err)
	}
	if err := m.Stop(context.Background(), "kona"); err != nil { // idempotent
		t.Fatal(err)
	}
	s.lag, s.unhealthy = 2, 3 // Cilium comes back after the nodes
	if err := m.Start(context.Background(), "kona", time.Minute); err != nil {
		t.Fatal(err)
	}
	if s.unhealthy >= 0 {
		t.Fatalf("Start returned before the DaemonSets were ready (%d polls left)", s.unhealthy)
	}
	if len(s.stopped) != 0 {
		t.Fatalf("not resumed: stopped=%v", s.stopped)
	}
	for n := range s.containers {
		if s.reported[n] != s.boot[n] {
			t.Fatalf("Start returned before %s re-registered", n)
		}
	}
	if infos, _ := m.List(context.Background()); infos[0].Status != StatusOK {
		t.Fatalf("list after start: %+v", infos[0])
	}
}

func TestStopAndStartOneNode(t *testing.T) {
	m, s, _, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if err := m.StopNodes(context.Background(), "kona", []string{"nope"}); err == nil {
		t.Fatal("unknown node accepted")
	}
	if err := m.StopNodes(context.Background(), "kona", []string{"kona-worker-1"}); err != nil {
		t.Fatal(err)
	}
	if len(s.stopped) != 1 || !s.stopped["kona-worker-1"] {
		t.Fatalf("stopped: %v", s.stopped)
	}
	if infos, _ := m.List(context.Background()); infos[0].Status != StatusDegraded {
		t.Fatalf("one node down is degraded, not stopped: %+v", infos[0])
	}
	s.lag = 2
	if err := m.StartNodes(context.Background(), "kona", []string{"kona-worker-1"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(s.stopped) != 0 || s.reported["kona-worker-1"] != s.boot["kona-worker-1"] {
		t.Fatalf("not started fresh: stopped=%v reported=%q boot=%q", s.stopped, s.reported["kona-worker-1"], s.boot["kona-worker-1"])
	}
}

func verifySetup(t *testing.T) (*Manager, *sim) {
	t.Helper()
	m, s, _, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	for _, n := range mustLoad(t, m, "kona").Nodes {
		s.ips[n.Name] = n.IP
	}
	s.clock = fmt.Sprintf("%d.0", m.Now().Unix())
	return m, s
}

func checks(t *testing.T, m *Manager) map[string]Check {
	t.Helper()
	r, err := m.Verify(context.Background(), "kona")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Check{}
	for _, c := range r.Checks {
		out[c.ID] = c
	}
	return out
}

func TestVerifyHealthyCluster(t *testing.T) {
	m, _ := verifySetup(t)
	r, err := m.Verify(context.Background(), "kona")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Checks {
		if c.Status != CheckPass {
			t.Errorf("%s: %s %s", c.ID, c.Status, c.Detail)
		}
	}
	if len(r.Checks) != 8 || r.Failed() != 0 {
		t.Fatalf("report: %+v", r)
	}
}

func TestVerifyFindsProblems(t *testing.T) {
	cases := []struct {
		name    string
		breakIt func(m *Manager, s *sim)
		want    map[string]string // check id -> status
	}{
		{"stopped VM with stale Ready", func(m *Manager, s *sim) { s.stopped["kona-worker-1"] = true },
			map[string]string{"vms": CheckFail, "nodes": CheckFail}},
		{"stale boot ID", func(m *Manager, s *sim) { s.boot["kona-worker-2"], s.lag = "boot-new", 99 },
			map[string]string{"vms": CheckPass, "nodes": CheckFail}},
		{"wrong node IP", func(m *Manager, s *sim) { s.ips["kona-worker-1"] = "192.168.64.9" },
			map[string]string{"nodes": CheckFail}},
		{"crash-looping pod", func(m *Manager, s *sim) { s.crashPod = true }, map[string]string{"pods": CheckFail}},
		{"DaemonSet not ready", func(m *Manager, s *sim) { s.dsNotReady = true }, map[string]string{"daemonsets": CheckFail}},
		{"host can't reach API", func(m *Manager, s *sim) { m.HostDial = func(string) error { return errors.New("timeout") } },
			map[string]string{"host-api": CheckFail, "api": CheckPass}},
		{"clock skew", func(m *Manager, s *sim) { s.clock = fmt.Sprintf("%d.0", m.Now().Unix()+5) }, map[string]string{"clock": CheckWarn}},
		{"API down", func(m *Manager, s *sim) { s.apiDown = true },
			map[string]string{"api": CheckFail, "nodes": CheckSkip, "clock": CheckSkip}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, s := verifySetup(t)
			tc.breakIt(m, s)
			got := checks(t, m)
			for id, want := range tc.want {
				if got[id].Status != want {
					t.Errorf("%s = %s (%s), want %s", id, got[id].Status, got[id].Detail, want)
				}
			}
		})
	}
}

func TestStartReportsUnhealthyCluster(t *testing.T) {
	m, s, _, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	m.StopNodes(context.Background(), "kona", []string{"kona-worker-1"})
	s.crashPod = true
	err := m.StartNodes(context.Background(), "kona", []string{"kona-worker-1"}, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "pods: kube-system/cilium-x (CrashLoopBackOff)") ||
		!strings.Contains(err.Error(), "kona verify cluster kona") {
		t.Fatalf("want verify failure, got %v", err)
	}
}
