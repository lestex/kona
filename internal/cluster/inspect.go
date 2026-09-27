package cluster

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lestex/kona/internal/runtime/container"
	"github.com/lestex/kona/internal/state"
)

// Cluster health values reported by List.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusOrphan   = "orphan"
)

// Info summarizes one cluster for `kona get clusters`.
type Info struct {
	Name    string   `json:"name"`
	Phase   string   `json:"phase"`
	Status  string   `json:"status"`
	Distro  string   `json:"distro,omitempty"`
	Subnet  string   `json:"subnet,omitempty"`
	Nodes   int      `json:"nodes"`
	Running int      `json:"running"`
	Issues  []string `json:"issues,omitempty"`
}

// List returns every cluster with state plus orphans: resources labeled
// kona.cluster=<name> with no state (for example after a crash mid-create).
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	states, err := m.Store.List()
	if err != nil {
		return nil, err
	}
	cs, err := m.Container.Containers(ctx)
	if err != nil {
		return nil, err
	}
	vs, err := m.Container.Volumes(ctx)
	if err != nil {
		return nil, err
	}
	ns, err := m.Container.Networks(ctx)
	if err != nil {
		return nil, err
	}
	running := map[string]map[string]bool{} // cluster -> container -> running
	labeled := map[string][]string{}        // cluster -> resource descriptions
	for _, c := range cs {
		cl := c.Configuration.Labels[container.LabelCluster]
		if cl == "" {
			continue
		}
		if running[cl] == nil {
			running[cl] = map[string]bool{}
		}
		running[cl][c.ID] = c.Status.State == "running"
		labeled[cl] = append(labeled[cl], "vm "+c.ID)
	}
	for _, v := range vs {
		if cl := v.Configuration.Labels[container.LabelCluster]; cl != "" {
			labeled[cl] = append(labeled[cl], "volume "+v.ID)
		}
	}
	for _, n := range ns {
		if cl := n.Configuration.Labels[container.LabelCluster]; cl != "" {
			labeled[cl] = append(labeled[cl], "network "+n.ID)
		}
	}

	var out []Info
	known := map[string]bool{}
	for _, s := range states {
		known[s.Name] = true
		i := Info{Name: s.Name, Phase: s.Phase, Status: StatusOK, Distro: s.Distro,
			Subnet: s.Network.Subnet, Nodes: len(s.Nodes)}
		for _, n := range s.Nodes {
			up, exists := running[s.Name][n.Name]
			switch {
			case !exists:
				i.Issues = append(i.Issues, n.Name+" missing")
			case !up:
				i.Issues = append(i.Issues, n.Name+" stopped")
			default:
				i.Running++
			}
		}
		for vm := range running[s.Name] {
			if _, ok := s.Node(vm); !ok {
				i.Issues = append(i.Issues, "orphan vm "+vm)
			}
		}
		if len(i.Issues) > 0 || s.Phase != state.PhaseReady {
			i.Status = StatusDegraded
		}
		sort.Strings(i.Issues)
		out = append(out, i)
	}
	for cl, res := range labeled {
		if known[cl] {
			continue
		}
		sort.Strings(res)
		i := Info{Name: cl, Phase: "-", Status: StatusOrphan, Issues: res}
		for _, up := range running[cl] {
			i.Nodes++
			if up {
				i.Running++
			}
		}
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

// NodeInfo describes one node for `kona get nodes`.
type NodeInfo struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Kind    string `json:"kind"`
	IP      string `json:"ip"`
	VM      string `json:"vm"`
	Ready   string `json:"ready"`
	Version string `json:"version,omitempty"`
	// SkewSeconds is guest clock minus host clock (positive: guest ahead).
	SkewSeconds *float64 `json:"skewSeconds,omitempty"`
}

// Nodes returns per-node status for a cluster. Kubernetes readiness and
// clock skew are best-effort: an unreachable API shows as "unknown".
func (m *Manager) Nodes(ctx context.Context, name string) ([]NodeInfo, error) {
	c, err := m.Store.Load(name)
	if err != nil {
		return nil, err
	}
	cs, err := m.containersOf(ctx, name)
	if err != nil {
		return nil, err
	}
	var kube map[string]NodeStatus
	if d, err := DistroFor(c.Distro); err == nil {
		kube, _ = m.KubeNodes(ctx, d, c)
	}
	var out []NodeInfo
	for _, n := range c.Nodes {
		ni := NodeInfo{Name: n.Name, Role: n.Role, Kind: n.Kind, IP: n.IP, VM: "missing", Ready: "unknown"}
		if ct, ok := cs[n.Name]; ok {
			ni.VM = ct.Status.State
		}
		if st, ok := kube[n.Name]; ok {
			ni.Ready, ni.Version = strconv.FormatBool(st.Ready), st.Version
		} else if kube != nil {
			ni.Ready = "not registered"
		}
		if ni.VM == "running" {
			if s, err := m.skew(ctx, n.Name); err == nil {
				ni.SkewSeconds = &s
			}
		}
		out = append(out, ni)
	}
	return out, nil
}

// skew measures guest-minus-host clock offset, taking the host time at the
// midpoint of the exec round-trip.
func (m *Manager) skew(ctx context.Context, node string) (float64, error) {
	before := m.Now()
	out, err := m.Container.Exec(ctx, node, "date", "+%s.%N")
	after := m.Now()
	if err != nil {
		return 0, err
	}
	guest, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("parse guest time %q: %w", out, err)
	}
	mid := before.Add(after.Sub(before) / 2)
	return guest - float64(mid.UnixNano())/float64(time.Second), nil
}
