// Package cluster creates, inspects and deletes kona clusters.
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lestex/kona/internal/cni"
	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/distro/k3s"
	"github.com/lestex/kona/internal/distro/kubeadm"
	"github.com/lestex/kona/internal/execx"
	"github.com/lestex/kona/internal/kubeconfig"
	"github.com/lestex/kona/internal/netplan"
	"github.com/lestex/kona/internal/runtime/container"
	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/tools"
)

const (
	secretsFile      = "secrets.json"
	legacyTokenFile  = "token"
	dataVolumeSize   = "10G"
	nodeMTU          = 1500
	pollInterval     = 2 * time.Second
	memoryWarnFactor = 0.75
)

// Manager holds dependencies for cluster operations.
type Manager struct {
	Store     *state.Store
	Container *container.Client
	// Host runs commands on macOS (e.g. the Cilium CLI).
	Host execx.Runner
	// Tools caches pinned helper binaries.
	Tools *tools.Cache
	Log   io.Writer
	// HostPrefixes returns host interface subnets (overridable in tests).
	HostPrefixes func() []netip.Prefix
	// Now and Sleep are overridable in tests.
	Now   func() time.Time
	Sleep func(context.Context, time.Duration) error
}

// NewManager returns a Manager with real host access.
func NewManager(s *state.Store, c *container.Client, host execx.Runner, log io.Writer) *Manager {
	return &Manager{Store: s, Container: c, Host: host, Tools: &tools.Cache{Dir: filepath.Join(s.Root, "bin")},
		Log: log, HostPrefixes: netplan.HostPrefixes, Now: time.Now, Sleep: sleep}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// CreateOptions configure a new cluster.
type CreateOptions struct {
	Name          string
	ControlPlanes int
	Workers       int
	GPUWorkers    int
	Distro        string
	CNI           string
	K8sVersion    string
	Image         string
	Kernel        string
	DNS           string
	CPUs          int
	Memory        string
	Wait          time.Duration
	Retain        bool
	HostMemory    int64
}

// DistroFor returns the Distro implementation for name.
func DistroFor(name string) (distro.Distro, error) {
	switch name {
	case "k3s":
		return k3s.K3s{}, nil
	case "kubeadm":
		return kubeadm.Kubeadm{}, nil
	default:
		return nil, fmt.Errorf("unknown distro %q (want k3s or kubeadm)", name)
	}
}

// DefaultImage returns the node image for a distro at the pinned version,
// or at k8sVersion when set.
func DefaultImage(distroName, k8sVersion string) string {
	d, err := DistroFor(distroName)
	if err != nil {
		return ""
	}
	img := d.DefaultImage()
	if k8sVersion == "" {
		return img
	}
	repo, tag, _ := strings.Cut(img, ":")
	switch distroName {
	case "k3s":
		if !strings.HasPrefix(tag, k8sVersion+"-") {
			tag = k8sVersion + "-k3s1"
		}
	case "kubeadm":
		tag = k8sVersion + "-kubeadm"
	}
	return repo + ":" + tag
}

// NetworkName is the `container` network of a cluster.
func NetworkName(cluster string) string { return "kona-" + cluster }

func (m *Manager) logf(format string, args ...any) {
	fmt.Fprintf(m.Log, format+"\n", args...)
}

// Create builds a cluster, or resumes one left by `create --retain`.
func (m *Manager) Create(ctx context.Context, o CreateOptions) (err error) {
	if err := validate(o); err != nil {
		return err
	}
	d, err := DistroFor(o.Distro)
	if err != nil {
		return err
	}
	c, err := m.Store.Load(o.Name)
	switch {
	case err == nil && c.Phase == state.PhaseReady:
		return fmt.Errorf("cluster %q already exists; delete it first with: kona delete cluster %s", o.Name, o.Name)
	case err == nil:
		m.logf("Resuming cluster %q (phase %s)", o.Name, c.Phase)
	case errors.Is(err, state.ErrNotFound):
		if c, err = m.plan(ctx, o); err != nil {
			return err
		}
	default:
		return err
	}
	m.warnMemory(c, o.HostMemory)

	defer func() {
		if err == nil {
			return
		}
		if o.Retain {
			c.Phase = state.PhaseFailed
			_ = m.Store.Save(c)
			err = fmt.Errorf("%w\ncluster %q retained for debugging; resume with `kona create cluster %s` or remove with `kona delete cluster %s`",
				err, o.Name, o.Name, o.Name)
			return
		}
		m.logf("✗ Create failed, rolling back (use --retain to keep the VMs)")
		if derr := m.Delete(context.WithoutCancel(ctx), o.Name); derr != nil {
			err = fmt.Errorf("%w (rollback also failed: %v; run `kona delete cluster %s`)", err, derr, o.Name)
		}
	}()

	deadline := m.Now().Add(o.Wait)
	secrets, err := m.secrets(c.Name, d)
	if err != nil {
		return err
	}
	if err := m.ensureNetwork(ctx, c); err != nil {
		return err
	}
	existing, err := m.containersOf(ctx, c.Name)
	if err != nil {
		return err
	}

	cps := c.ControlPlanes()
	if err := m.ensureNode(ctx, d, c, cps[0], secrets, existing); err != nil {
		return err
	}
	m.logf("• Waiting for the API server on %s", cps[0].IP)
	if err := m.waitAPI(ctx, d, c, deadline); err != nil {
		return err
	}
	if c.CNI == "cilium" {
		// Nodes only become Ready once the CNI runs, so Cilium goes in
		// before the remaining nodes join.
		if err := m.installCilium(ctx, d, c); err != nil {
			return err
		}
	}
	for _, n := range cps[1:] { // etcd members join one at a time
		if err := m.ensureNode(ctx, d, c, n, secrets, existing); err != nil {
			return err
		}
		if err := m.waitNodesReady(ctx, d, c, nodeNames(cps), deadline, false); err != nil {
			return err
		}
	}
	for _, n := range c.Nodes {
		if n.Role == state.RoleControlPlane {
			continue
		}
		if err := m.ensureNode(ctx, d, c, n, secrets, existing); err != nil {
			return err
		}
	}
	m.logf("• Waiting up to %s for %d nodes to be Ready", time.Until(deadline).Round(time.Second), len(c.Nodes))
	if err := m.waitNodesReady(ctx, d, c, nodeNames(c.Nodes), deadline, true); err != nil {
		return err
	}
	if c.CNI == "cilium" {
		if err := m.waitCilium(ctx, c, deadline); err != nil {
			return err
		}
	}
	path, err := m.WriteKubeconfig(ctx, c)
	if err != nil {
		return err
	}
	c.Phase = state.PhaseReady
	if err := m.Store.Save(c); err != nil {
		return err
	}
	m.logf("✓ Cluster %q is ready\n\n  kubectl --kubeconfig %s get nodes\n", c.Name, path)
	return nil
}

func validate(o CreateOptions) error {
	if err := state.ValidateName(o.Name); err != nil {
		return err
	}
	if _, err := DistroFor(o.Distro); err != nil {
		return err
	}
	if o.ControlPlanes < 1 {
		return errors.New("--control-planes must be at least 1")
	}
	if o.ControlPlanes > 1 && o.ControlPlanes%2 == 0 {
		return fmt.Errorf("--control-planes %d: use an odd number for etcd quorum", o.ControlPlanes)
	}
	if o.Workers < 0 {
		return errors.New("--workers must be >= 0")
	}
	if o.GPUWorkers > 0 {
		return errors.New("--gpu-workers is not implemented yet (Phase 3)")
	}
	switch o.CNI {
	case "", "default", "cilium":
	default:
		return fmt.Errorf("unknown --cni %q (want default or cilium)", o.CNI)
	}
	if o.Kernel == "" {
		return errors.New("no guest kernel configured")
	}
	if _, err := os.Stat(o.Kernel); err != nil {
		return fmt.Errorf("guest kernel %s: %w (run `kona doctor`)", o.Kernel, err)
	}
	if _, err := ParseSize(o.Memory); err != nil {
		return err
	}
	if o.Wait <= 0 {
		return errors.New("--wait must be positive")
	}
	return nil
}

// plan allocates the subnet, node names, IPs and MACs and saves the state.
func (m *Manager) plan(ctx context.Context, o CreateOptions) (*state.Cluster, error) {
	used := m.HostPrefixes()
	nets, err := m.Container.Networks(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nets {
		if p, err := netip.ParsePrefix(n.Subnet()); err == nil {
			used = append(used, p)
		}
	}
	others, err := m.Store.List()
	if err != nil {
		return nil, err
	}
	for _, oc := range others {
		if p, err := netip.ParsePrefix(oc.Network.Subnet); err == nil {
			used = append(used, p)
		}
	}
	subnet, err := netplan.PickSubnet(used)
	if err != nil {
		return nil, err
	}
	c := &state.Cluster{
		Name: o.Name, Distro: o.Distro, CNI: cniName(o.CNI), K8sVersion: o.K8sVersion,
		Image: o.Image, Kernel: o.Kernel, DNS: o.DNS, Phase: state.PhaseCreating,
		Network: state.Network{Name: NetworkName(o.Name), Subnet: subnet.String(),
			Gateway: netplan.Gateway(subnet).String(), Prefix: netplan.Prefix},
		CreatedAt: m.Now().UTC(),
	}
	add := func(role string, i int, ip netip.Addr, volume bool) {
		name := fmt.Sprintf("%s-%s-%d", o.Name, role, i)
		n := state.Node{Name: name, Role: role, Kind: state.KindContainer, IP: ip.String(),
			MAC: netplan.MAC(o.Name, name), CPUs: o.CPUs, Memory: o.Memory}
		if volume {
			n.Volume = name + "-data"
		}
		c.Nodes = append(c.Nodes, n)
	}
	for i := 1; i <= o.ControlPlanes; i++ {
		ip, err := netplan.ControlPlaneIP(subnet, i)
		if err != nil {
			return nil, err
		}
		add(state.RoleControlPlane, i, ip, true)
	}
	for i := 1; i <= o.Workers; i++ {
		ip, err := netplan.WorkerIP(subnet, i)
		if err != nil {
			return nil, err
		}
		add(state.RoleWorker, i, ip, false)
	}
	m.logf("• Planned cluster %q: %d control plane(s), %d worker(s) on %s", o.Name, o.ControlPlanes, o.Workers, subnet)
	return c, m.Store.Save(c)
}

func (m *Manager) warnMemory(c *state.Cluster, hostMem int64) {
	if hostMem <= 0 {
		return
	}
	var total int64
	for _, n := range c.Nodes {
		b, _ := ParseSize(n.Memory)
		total += b
	}
	if float64(total) > memoryWarnFactor*float64(hostMem) {
		m.logf("! Nodes reserve %s of fixed memory, more than 75%% of this Mac's %s (VMs do not balloon)",
			FormatSize(total), FormatSize(hostMem))
	}
}

func (m *Manager) secrets(cluster string, d distro.Distro) (distro.Secrets, error) {
	if b, err := m.Store.ReadSecret(cluster, secretsFile); err == nil {
		var s distro.Secrets
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, fmt.Errorf("parse %s: %w", secretsFile, err)
		}
		return s, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// Clusters created before secrets.json kept the k3s token alone.
	if b, err := m.Store.ReadSecret(cluster, legacyTokenFile); err == nil {
		return distro.Secrets{k3s.SecretToken: strings.TrimSpace(string(b))}, nil
	}
	s, err := d.NewSecrets()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return s, m.Store.WriteSecret(cluster, secretsFile, b)
}

func (m *Manager) ensureNetwork(ctx context.Context, c *state.Cluster) error {
	nets, err := m.Container.Networks(ctx)
	if err != nil {
		return err
	}
	for _, n := range nets {
		if n.ID == c.Network.Name {
			if n.Subnet() != c.Network.Subnet {
				return fmt.Errorf("network %s exists with subnet %s, state says %s", n.ID, n.Subnet(), c.Network.Subnet)
			}
			return nil
		}
	}
	m.logf("• Creating network %s (%s)", c.Network.Name, c.Network.Subnet)
	return m.Container.CreateNetwork(ctx, c.Network.Name, c.Network.Subnet,
		map[string]string{container.LabelCluster: c.Name})
}

func (m *Manager) containersOf(ctx context.Context, cluster string) (map[string]container.Container, error) {
	cs, err := m.Container.Containers(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]container.Container{}
	for _, c := range cs {
		if c.Configuration.Labels[container.LabelCluster] == cluster {
			out[c.ID] = c
		}
	}
	return out, nil
}

func (m *Manager) ensureNode(ctx context.Context, d distro.Distro, c *state.Cluster, n state.Node,
	secrets distro.Secrets, existing map[string]container.Container) error {
	spec, err := d.NodeSpec(c, n, secrets)
	if err != nil {
		return err
	}
	if ex, ok := existing[n.Name]; ok {
		if ex.Status.State == "running" {
			m.logf("✓ Node %s already running", n.Name)
			return d.Bootstrap(ctx, m.Container, c, n, secrets)
		}
		if err := m.Container.Delete(ctx, n.Name); err != nil {
			return err
		}
	}
	labels := map[string]string{container.LabelCluster: c.Name, container.LabelRole: n.Role}
	vols := map[string]string{}
	if n.Volume != "" {
		have, err := m.Container.Volumes(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, v := range have {
			found = found || v.ID == n.Volume
		}
		if !found {
			if err := m.Container.CreateVolume(ctx, n.Volume, dataVolumeSize, labels); err != nil {
				return err
			}
		}
		vols[n.Volume] = spec.DataDir
	}
	env := map[string]string{"KONA_NODE_IP": fmt.Sprintf("%s/%d", n.IP, c.Network.Prefix)}
	for k, v := range spec.Env {
		env[k] = v
	}
	m.logf("• Starting %s (%s, %d CPU, %s)", n.Name, n.IP, n.CPUs, n.Memory)
	if err := m.Container.Run(ctx, container.RunSpec{
		Name: n.Name, Init: spec.UseInit, Tmpfs: spec.Tmpfs, Image: c.Image, Kernel: c.Kernel,
		Network: c.Network.Name, MAC: n.MAC, MTU: nodeMTU, DNS: c.DNS, CPUs: n.CPUs, Memory: n.Memory,
		Env: env, Labels: labels, Volumes: vols, Args: spec.Args,
	}); err != nil {
		return err
	}
	return d.Bootstrap(ctx, m.Container, c, n, secrets)
}

func (m *Manager) waitAPI(ctx context.Context, d distro.Distro, c *state.Cluster, deadline time.Time) error {
	cp := c.ControlPlanes()[0].Name
	var last error
	for m.Now().Before(deadline) {
		out, err := m.Container.Exec(ctx, cp, d.Kubectl("get", "--raw", "/readyz")...)
		if err == nil && strings.TrimSpace(string(out)) == "ok" {
			return nil
		}
		last = err
		if err := m.Sleep(ctx, pollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("API server on %s not ready before --wait expired: %s (see `container logs %s`)", cp, lastLine(last), cp)
}

// NodeStatus is one node's Kubernetes view.
type NodeStatus struct {
	Ready   bool
	Version string
}

// KubeNodes returns the Kubernetes status of every registered node.
func (m *Manager) KubeNodes(ctx context.Context, d distro.Distro, c *state.Cluster) (map[string]NodeStatus, error) {
	cps := c.ControlPlanes()
	if len(cps) == 0 {
		return nil, errors.New("no control plane")
	}
	out, err := m.Container.Exec(ctx, cps[0].Name, d.Kubectl("get", "nodes", "-o", "json")...)
	if err != nil {
		return nil, err
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
				NodeInfo struct {
					KubeletVersion string `json:"kubeletVersion"`
				} `json:"nodeInfo"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse node list: %w", err)
	}
	res := map[string]NodeStatus{}
	for _, it := range list.Items {
		st := NodeStatus{Version: it.Status.NodeInfo.KubeletVersion}
		for _, cond := range it.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				st.Ready = true
			}
		}
		res[it.Metadata.Name] = st
	}
	return res, nil
}

func (m *Manager) waitNodesReady(ctx context.Context, d distro.Distro, c *state.Cluster, names []string,
	deadline time.Time, report bool) error {
	var notReady []string
	for m.Now().Before(deadline) {
		st, err := m.KubeNodes(ctx, d, c)
		notReady = notReady[:0]
		for _, n := range names {
			if err != nil || !st[n].Ready {
				notReady = append(notReady, n)
			}
		}
		if len(notReady) == 0 {
			if report {
				m.logf("✓ All %d nodes Ready", len(names))
			}
			return nil
		}
		if err := m.Sleep(ctx, pollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("nodes not Ready before --wait expired: %s (see `container logs <node>`)", strings.Join(notReady, ", "))
}

// WriteKubeconfig fetches the admin kubeconfig and writes ~/.kube/kona-<name>.
func (m *Manager) WriteKubeconfig(ctx context.Context, c *state.Cluster) (string, error) {
	raw, err := m.Kubeconfig(ctx, c)
	if err != nil {
		return "", err
	}
	path, err := kubeconfig.Path(c.Name)
	if err != nil {
		return "", err
	}
	if err := kubeconfig.Write(path, raw); err != nil {
		return "", err
	}
	m.logf("✓ Wrote kubeconfig %s (context %s)", path, kubeconfig.ContextName(c.Name))
	return path, nil
}

// Kubeconfig returns the host-usable admin kubeconfig for c.
func (m *Manager) Kubeconfig(ctx context.Context, c *state.Cluster) ([]byte, error) {
	d, err := DistroFor(c.Distro)
	if err != nil {
		return nil, err
	}
	raw, err := d.AdminKubeconfig(ctx, m.Container, c)
	if err != nil {
		return nil, fmt.Errorf("read admin kubeconfig: %w", err)
	}
	return kubeconfig.Rewrite(raw, c.Name, "https://"+c.ControlPlanes()[0].IP+":6443")
}

// Delete removes every resource of a cluster. It is idempotent and works
// from labels, so it also cleans up clusters whose state is gone.
func (m *Manager) Delete(ctx context.Context, name string) error {
	var errs []error
	cs, err := m.Container.Containers(ctx)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if c.Configuration.Labels[container.LabelCluster] == name {
			m.logf("• Deleting node %s", c.ID)
			errs = append(errs, m.Container.Delete(ctx, c.ID))
		}
	}
	vs, err := m.Container.Volumes(ctx)
	if err != nil {
		return err
	}
	for _, v := range vs {
		if v.Configuration.Labels[container.LabelCluster] == name {
			m.logf("• Deleting volume %s", v.ID)
			errs = append(errs, m.Container.DeleteVolume(ctx, v.ID))
		}
	}
	errs = append(errs, m.Container.DeleteNetwork(ctx, NetworkName(name)))
	if p, err := kubeconfig.Path(name); err == nil {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		// Only kona-<name> entries are removed, and only if --merge put them there.
		errs = append(errs, kubeconfig.Remove(strings.TrimSuffix(p, "kona-"+name)+"config", name))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return m.Store.Delete(name)
}

func cniName(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

func (m *Manager) installCilium(ctx context.Context, d distro.Distro, c *state.Cluster) error {
	cp := c.ControlPlanes()[0].Name
	if _, err := m.Container.Exec(ctx, cp, d.Kubectl("-n", "kube-system", "get", "daemonset", "cilium")...); err == nil {
		m.logf("✓ Cilium already installed")
		return nil
	}
	kc, err := m.WriteKubeconfig(ctx, c)
	if err != nil {
		return err
	}
	cli, err := m.Tools.Ensure(ctx, tools.CiliumCLI())
	if err != nil {
		return fmt.Errorf("cilium CLI: %w", err)
	}
	m.logf("• Installing Cilium %s (kube-proxy replacement, VXLAN, Hubble)", cni.CiliumVersion())
	if _, err := m.Host.Run(ctx, cli, cni.CiliumInstallArgs(c, kc, kubeconfig.ContextName(c.Name))...); err != nil {
		return fmt.Errorf("cilium install: %w", err)
	}
	return nil
}

func (m *Manager) waitCilium(ctx context.Context, c *state.Cluster, deadline time.Time) error {
	kc, err := kubeconfig.Path(c.Name)
	if err != nil {
		return err
	}
	cli, err := m.Tools.Ensure(ctx, tools.CiliumCLI())
	if err != nil {
		return err
	}
	left := time.Until(deadline).Round(time.Second)
	if left < 10*time.Second {
		left = 10 * time.Second
	}
	m.logf("• Waiting up to %s for Cilium to be healthy", left)
	if _, err := m.Host.Run(ctx, cli, cni.CiliumStatusArgs(kc, kubeconfig.ContextName(c.Name), left.String())...); err != nil {
		return fmt.Errorf("cilium status: %s", lastLine(err))
	}
	m.logf("✓ Cilium is healthy")
	return nil
}

// lastLine returns the last non-empty line of err: tools like k3s print
// progress logs before the actual error.
func lastLine(err error) string {
	if err == nil {
		return "no response"
	}
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func nodeNames(ns []state.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Name
	}
	return out
}

// ParseSize parses sizes like 2G, 512M, 2048 (MiB) into bytes.
func ParseSize(s string) (int64, error) {
	if s == "" {
		return 0, errors.New("empty memory size")
	}
	mult := int64(1 << 20)
	num := strings.ToUpper(strings.TrimSuffix(strings.TrimSuffix(s, "B"), "i"))
	switch {
	case strings.HasSuffix(num, "G"):
		mult, num = 1<<30, strings.TrimSuffix(num, "G")
	case strings.HasSuffix(num, "M"):
		mult, num = 1<<20, strings.TrimSuffix(num, "M")
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("invalid memory size %q (examples: 2G, 1536M)", s)
	}
	return int64(v * float64(mult)), nil
}

// FormatSize renders bytes as GiB.
func FormatSize(b int64) string {
	return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
}
