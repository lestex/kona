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

	"github.com/lestex/kona/internal/execx"
	"github.com/lestex/kona/internal/runtime/container"
	"github.com/lestex/kona/internal/state"
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
				for n := range s.containers {
					st := "True"
					if s.notReady {
						st = "False"
					}
					items = append(items, fmt.Sprintf(`{"metadata":{"name":%q},"status":{"conditions":[{"type":"Ready","status":%q}]}}`, n, st))
				}
				return ok(`{"items":[` + strings.Join(items, ",") + `]}`)
			case strings.HasPrefix(cmd, "cat "):
				return ok(adminKubeconfig)
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
	m := &Manager{
		Store: &state.Store{Root: t.TempDir()}, Container: container.New(s.runner()), Log: &log,
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
		func(o *CreateOptions) { o.CNI = "cilium" },
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
