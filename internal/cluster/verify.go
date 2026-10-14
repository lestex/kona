package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/procx"
	"github.com/lestex/kona/internal/state"
)

// Check outcomes. Only fail makes `kona verify cluster` exit non-zero.
const (
	CheckPass = "pass"
	CheckWarn = "warn"
	CheckFail = "fail"
	CheckSkip = "skip"
)

// MaxClockSkew is the guest/host clock offset above which verify warns.
const MaxClockSkew = time.Second

// Check is one independent health signal.
type Check struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

// Report is the result of Verify.
type Report struct {
	Cluster string  `json:"cluster"`
	Checks  []Check `json:"checks"`
}

// Failed returns the number of failed checks.
func (r Report) Failed() int {
	n := 0
	for _, c := range r.Checks {
		if c.Status == CheckFail {
			n++
		}
	}
	return n
}

func (r *Report) add(id, status, detail, hint string) {
	r.Checks = append(r.Checks, Check{ID: id, Status: status, Detail: detail, Hint: hint})
}

// kubeObject holds the fields verify reads from nodes, pods, DaemonSets,
// Deployments and ResourceSlices.
type kubeObject struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Labels    map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		NodeName string `json:"nodeName"`
	} `json:"spec"`
	Status struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
		Addresses []struct {
			Type    string `json:"type"`
			Address string `json:"address"`
		} `json:"addresses"`
		NodeInfo struct {
			BootID string `json:"bootID"`
		} `json:"nodeInfo"`
		Phase             string `json:"phase"`
		ContainerStatuses []struct {
			State struct {
				Waiting *struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
			} `json:"state"`
		} `json:"containerStatuses"`
		DesiredNumberScheduled int `json:"desiredNumberScheduled"`
		NumberReady            int `json:"numberReady"`
		ReadyReplicas          int `json:"readyReplicas"`
	} `json:"status"`
}

// badWaiting are container waiting reasons that never clear on their own.
var badWaiting = map[string]bool{"CrashLoopBackOff": true, "ImagePullBackOff": true, "ErrImagePull": true,
	"ErrImageNeverPull": true, "CreateContainerConfigError": true, "CreateContainerError": true}

// Verify inspects a cluster without changing it.
func (m *Manager) Verify(ctx context.Context, name string) (Report, error) {
	r := Report{Cluster: name}
	c, err := m.Store.Load(name)
	if err != nil {
		return r, err
	}
	d, err := DistroFor(c.Distro)
	if err != nil {
		return r, err
	}
	startHint := "kona start cluster " + name

	// VMs and relays.
	cs, err := m.containersOf(ctx, name)
	if err != nil {
		return r, err
	}
	var down []string
	running := map[string]bool{}
	for _, n := range c.Nodes {
		up := false
		if n.Kind == state.KindKrunkit {
			up = m.gpuRunning(c, n)
		} else if ct, ok := cs[n.Name]; ok {
			up = ct.Status.State == "running"
		}
		running[n.Name] = up
		if !up {
			down = append(down, n.Name)
		}
	}
	if len(down) > 0 {
		r.add("vms", CheckFail, "not running: "+strings.Join(down, ", "), startHint)
	} else {
		r.add("vms", CheckPass, fmt.Sprintf("%d node VMs running", len(c.Nodes)), "")
	}
	switch {
	case len(c.GPUNodes()) == 0:
		r.add("relays", CheckSkip, "no GPU nodes", "")
	case c.GPULink() != state.GPULinkRelay:
		r.add("relays", CheckSkip, "--gpu-link "+c.GPULink(), "")
	default:
		var dead []string
		for _, n := range c.CPUNodes() {
			if n.WGPort != 0 && !m.relayRunning(c, n) {
				dead = append(dead, n.Name)
			}
		}
		if len(dead) > 0 {
			r.add("relays", CheckFail, "no WireGuard relay for: "+strings.Join(dead, ", "), startHint)
		} else {
			r.add("relays", CheckPass, fmt.Sprintf("%d WireGuard relays running", len(c.CPUNodes())), "")
		}
	}

	// API, from the host (what kubectl uses) and inside the control plane.
	cp := c.ControlPlanes()[0]
	if err := m.hostDial(net.JoinHostPort(cp.IP, "6443")); err != nil {
		r.add("host-api", CheckFail, err.Error(), "the host can't reach the API; check `container network list` and the node's IP")
	} else {
		r.add("host-api", CheckPass, cp.IP+":6443 reachable from the host", "")
	}
	if _, err := m.Container.Exec(ctx, cp.Name, d.Kubectl("get", "--raw", "/readyz")...); err != nil {
		r.add("api", CheckFail, "readyz: "+lastLine(err), "see `container logs "+cp.Name+"`")
		for _, id := range []string{"nodes", "pods", "daemonsets", "dns", "gpu"} {
			r.add(id, CheckSkip, "API not ready", "")
		}
		r.add("clock", CheckSkip, "API not ready", "")
		return r, nil
	}
	r.add("api", CheckPass, "readyz ok", "")

	out, err := m.Container.Exec(ctx, cp.Name, d.Kubectl("get", "nodes,pods,daemonsets,deployments", "-A", "-o", "json")...)
	if err != nil {
		return r, fmt.Errorf("list cluster objects: %w", err)
	}
	var list struct {
		Items []kubeObject `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return r, fmt.Errorf("parse cluster objects: %w", err)
	}
	byKind := map[string][]kubeObject{}
	for _, o := range list.Items {
		byKind[o.Kind] = append(byKind[o.Kind], o)
	}

	m.verifyNodes(ctx, &r, c, byKind["Node"], running)
	m.verifyWorkloads(&r, byKind)
	m.verifyClock(ctx, &r, c, running)
	m.verifyGPU(ctx, &r, d, c, byKind["Node"])
	return r, nil
}

func (m *Manager) verifyNodes(ctx context.Context, r *Report, c *state.Cluster, nodes []kubeObject, running map[string]bool) {
	kube := map[string]kubeObject{}
	for _, o := range nodes {
		kube[o.Metadata.Name] = o
	}
	var problems []string
	for _, n := range c.Nodes {
		o, ok := kube[n.Name]
		if !ok {
			problems = append(problems, n.Name+" not registered")
			continue
		}
		if !conditionTrue(o, "Ready") {
			problems = append(problems, n.Name+" not Ready")
			continue
		}
		if !running[n.Name] {
			problems = append(problems, n.Name+" shows Ready but its VM is down (stale status)")
			continue
		}
		for _, a := range o.Status.Addresses {
			if a.Type == "InternalIP" && a.Address != n.IP {
				problems = append(problems, fmt.Sprintf("%s InternalIP %s, want %s", n.Name, a.Address, n.IP))
			}
		}
		// A Node stays Ready for a while after its VM stops; a boot ID
		// that no longer matches the VM means the kubelet hasn't reported
		// since the last boot.
		if boot, err := m.bootID(ctx, c, n); err == nil && boot != o.Status.NodeInfo.BootID {
			problems = append(problems, n.Name+" Ready is stale (kubelet hasn't reported since the VM booted)")
		}
	}
	if len(problems) > 0 {
		r.add("nodes", CheckFail, strings.Join(problems, "; "), "see `kubectl describe node <node>`")
		return
	}
	r.add("nodes", CheckPass, fmt.Sprintf("%d nodes Ready on their kona IPs", len(c.Nodes)), "")
}

func (m *Manager) verifyWorkloads(r *Report, byKind map[string][]kubeObject) {
	var bad []string
	for _, p := range byKind["Pod"] {
		if ns := p.Metadata.Namespace; ns != "kube-system" && ns != "kona-system" {
			continue
		}
		id := p.Metadata.Namespace + "/" + p.Metadata.Name
		reason := ""
		for _, cs := range p.Status.ContainerStatuses {
			if w := cs.State.Waiting; w != nil && badWaiting[w.Reason] {
				reason = w.Reason
			}
		}
		switch {
		case reason != "":
			bad = append(bad, id+" ("+reason+")")
		case p.Status.Phase != "Running" && p.Status.Phase != "Succeeded":
			bad = append(bad, id+" ("+p.Status.Phase+")")
		case p.Status.Phase == "Running" && !conditionTrue(p, "Ready"):
			bad = append(bad, id+" (not Ready)")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		r.add("pods", CheckFail, strings.Join(bad, ", "), "see `kubectl -n <namespace> describe pod <pod>`")
	} else {
		r.add("pods", CheckPass, "kube-system and kona-system pods healthy", "")
	}

	var notReady, names []string
	for _, ds := range byKind["DaemonSet"] {
		names = append(names, ds.Metadata.Name)
		if ds.Status.NumberReady < ds.Status.DesiredNumberScheduled {
			notReady = append(notReady, fmt.Sprintf("%s/%s %d/%d", ds.Metadata.Namespace, ds.Metadata.Name,
				ds.Status.NumberReady, ds.Status.DesiredNumberScheduled))
		}
	}
	sort.Strings(notReady)
	if len(notReady) > 0 {
		r.add("daemonsets", CheckFail, "not ready: "+strings.Join(notReady, ", "), "see `kubectl -n <namespace> rollout status ds/<name>`")
	} else {
		sort.Strings(names)
		r.add("daemonsets", CheckPass, "ready: "+strings.Join(names, ", "), "")
	}

	for _, dep := range byKind["Deployment"] {
		if dep.Metadata.Namespace == "kube-system" && dep.Metadata.Name == "coredns" {
			if dep.Status.ReadyReplicas > 0 {
				r.add("dns", CheckPass, fmt.Sprintf("coredns %d ready", dep.Status.ReadyReplicas), "")
			} else {
				r.add("dns", CheckFail, "coredns has no ready replica", "see `kubectl -n kube-system logs deploy/coredns`")
			}
			return
		}
	}
	r.add("dns", CheckFail, "no kube-system/coredns Deployment", "")
}

func (m *Manager) verifyClock(ctx context.Context, r *Report, c *state.Cluster, running map[string]bool) {
	var skewed, unknown []string
	worst := 0.0
	for _, n := range c.Nodes {
		if !running[n.Name] {
			continue
		}
		s, err := m.skew(ctx, c, n)
		if err != nil {
			unknown = append(unknown, n.Name)
			continue
		}
		worst = math.Max(worst, math.Abs(s))
		if math.Abs(s) > MaxClockSkew.Seconds() {
			skewed = append(skewed, fmt.Sprintf("%s %+.2fs", n.Name, s))
		}
	}
	switch {
	case len(skewed) > 0:
		r.add("clock", CheckWarn, "skew: "+strings.Join(skewed, ", "), "chrony should step the clock; see `kona get nodes`")
	case len(unknown) > 0:
		r.add("clock", CheckWarn, "could not read the clock of: "+strings.Join(unknown, ", "), "")
	default:
		r.add("clock", CheckPass, fmt.Sprintf("worst skew %.2fs", worst), "")
	}
}

func (m *Manager) verifyGPU(ctx context.Context, r *Report, d distro.Distro, c *state.Cluster, nodes []kubeObject) {
	gpus := c.GPUNodes()
	if len(gpus) == 0 {
		r.add("gpu", CheckSkip, "no GPU nodes", "")
		return
	}
	labels := map[string]string{}
	for _, o := range nodes {
		labels[o.Metadata.Name] = o.Metadata.Labels[LabelGPU]
	}
	out, err := m.Container.Exec(ctx, c.ControlPlanes()[0].Name, d.Kubectl("get", "resourceslices", "-o", "json")...)
	if err != nil {
		r.add("gpu", CheckFail, "list ResourceSlices: "+lastLine(err), "")
		return
	}
	var slices struct {
		Items []kubeObject `json:"items"`
	}
	if err := json.Unmarshal(out, &slices); err != nil {
		r.add("gpu", CheckFail, "parse ResourceSlices: "+err.Error(), "")
		return
	}
	published := map[string]bool{}
	for _, s := range slices.Items {
		published[s.Spec.NodeName] = true
	}
	var fail, warn []string
	for _, n := range gpus {
		switch labels[n.Name] {
		case GPUAvailable:
			if !published[n.Name] {
				fail = append(fail, n.Name+" has no ResourceSlice")
			}
		case GPUUnavailable:
			warn = append(warn, n.Name+" has no Venus GPU (joined as a CPU node)")
		default:
			fail = append(fail, n.Name+" has no "+LabelGPU+" label")
		}
	}
	switch {
	case len(fail) > 0:
		r.add("gpu", CheckFail, strings.Join(append(fail, warn...), "; "), "see `kubectl -n kona-system logs ds/kona-venus-dra`")
	case len(warn) > 0:
		r.add("gpu", CheckWarn, strings.Join(warn, "; "), "see docs/gpu.md")
	default:
		r.add("gpu", CheckPass, fmt.Sprintf("%d GPU node(s) publish a Venus ResourceSlice", len(gpus)), "")
	}
}

func conditionTrue(o kubeObject, typ string) bool {
	for _, c := range o.Status.Conditions {
		if c.Type == typ {
			return c.Status == "True"
		}
	}
	return false
}

func (m *Manager) relayRunning(c *state.Cluster, n state.Node) bool {
	if m.RelayRunning != nil {
		return m.RelayRunning(c, n)
	}
	pid, ok := procx.ReadPid(filepath.Join(m.nodeDir(c.Name, n.Name), "relay.pid"))
	return ok && procx.Alive(pid, relayTag(c.Name))
}

func (m *Manager) hostDial(addr string) error {
	if m.HostDial != nil {
		return m.HostDial(addr)
	}
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}
