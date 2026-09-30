package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/distro/k3s"
	"github.com/lestex/kona/internal/netplan"
	"github.com/lestex/kona/internal/runtime/krunkit"
	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/version"
	"github.com/lestex/kona/internal/wg"
)

// GPU node labels.
const (
	LabelGPU            = "kona.dev/gpu"
	GPUAvailable        = "venus"
	GPUUnavailable      = "unavailable"
	sshKeyFile          = "ssh_ed25519"
	defaultGPUMemoryMiB = 4096
)

// DefaultGPUImageName is the GPU node disk image for the pinned k3s.
func DefaultGPUImageName() string {
	return "kona-gpu-node-" + strings.ReplaceAll(version.Get("K3S_VERSION"), "+", "-") + ".raw"
}

// RunDir is where a cluster's unix sockets live (short path).
func RunDir(cluster string) string {
	return filepath.Join("/tmp", fmt.Sprintf("kona-%d", os.Getuid()), cluster)
}

func (m *Manager) nodeDir(cluster, node string) string {
	return filepath.Join(m.Store.Dir(cluster), "nodes", node)
}

func (m *Manager) krunkitNode(c *state.Cluster, n state.Node) krunkit.Node {
	mem, _ := ParseSize(n.Memory)
	mib := mem >> 20
	if mib == 0 {
		mib = defaultGPUMemoryMiB
	}
	subnet := netip.MustParsePrefix(c.GPUNetwork.Subnet)
	last := subnet.Masked().Addr().As4()
	last[3] = 254
	return krunkit.Node{
		Name: n.Name, Dir: m.nodeDir(c.Name, n.Name), RunDir: RunDir(c.Name),
		MAC: n.MAC, CPUs: n.CPUs, MemoryMiB: mib,
		Gateway: c.GPUNetwork.Gateway, SubnetEnd: netip.AddrFrom4(last).String(), Netmask: "255.255.255.0",
	}
}

// wgKey returns (creating if needed) node's WireGuard private key.
func (m *Manager) wgKey(cluster, node string, s distro.Secrets) (string, error) {
	k := "wg:" + node
	if s[k] != "" {
		return s[k], nil
	}
	key, err := wg.GenerateKey()
	if err != nil {
		return "", err
	}
	s[k] = key
	return key, m.saveSecrets(cluster, s)
}

func (m *Manager) saveSecrets(cluster string, s distro.Secrets) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return m.Store.WriteSecret(cluster, secretsFile, b)
}

// wgConfigs returns every node's WireGuard config: CPU nodes peer with all
// GPU nodes (endpoints learned), GPU nodes dial each CPU node's published
// port at their own vmnet gateway.
func (m *Manager) wgConfigs(c *state.Cluster, s distro.Secrets) (map[string]wg.Config, error) {
	pub := map[string]string{}
	priv := map[string]string{}
	for _, n := range c.Nodes {
		k, err := m.wgKey(c.Name, n.Name, s)
		if err != nil {
			return nil, err
		}
		p, err := wg.PublicKey(k)
		if err != nil {
			return nil, err
		}
		priv[n.Name], pub[n.Name] = k, p
	}
	out := map[string]wg.Config{}
	for _, cpu := range c.CPUNodes() {
		cfg := wg.Config{PrivateKey: priv[cpu.Name], Routes: []string{c.GPUNetwork.Subnet}}
		for _, g := range c.GPUNodes() {
			cfg.Peers = append(cfg.Peers, wg.Peer{Name: g.Name, PublicKey: pub[g.Name], IP: g.IP})
		}
		out[cpu.Name] = cfg
	}
	for _, g := range c.GPUNodes() {
		cfg := wg.Config{PrivateKey: priv[g.Name], Routes: []string{c.Network.Subnet}}
		for _, cpu := range c.CPUNodes() {
			if cpu.WGPort == 0 {
				return nil, fmt.Errorf("node %s has no published WireGuard port: cluster %q predates GPU support; recreate it", cpu.Name, c.Name)
			}
			cfg.Peers = append(cfg.Peers, wg.Peer{Name: cpu.Name, PublicKey: pub[cpu.Name], IP: cpu.IP,
				Endpoint: fmt.Sprintf("%s:%d", c.GPUNetwork.Gateway, cpu.WGPort)})
		}
		out[g.Name] = cfg
	}
	return out, nil
}

// syncCPUWireGuard pushes the WireGuard config to every running CPU node.
func (m *Manager) syncCPUWireGuard(ctx context.Context, c *state.Cluster, cfgs map[string]wg.Config) error {
	for _, n := range c.CPUNodes() {
		conf, routes := cfgs[n.Name].Render()
		if err := m.writeNodeFile(ctx, n.Name, "/etc/kona/wg0.conf", conf, "0600"); err != nil {
			return err
		}
		if err := m.writeNodeFile(ctx, n.Name, "/etc/kona/wg-routes", routes, "0644"); err != nil {
			return err
		}
		if _, err := m.Container.Exec(ctx, n.Name, "/usr/local/bin/kona-wg"); err != nil {
			return fmt.Errorf("configure WireGuard on %s: %w", n.Name, err)
		}
	}
	return nil
}

// writeNodeFile writes content into a `container` node via base64, so no
// shell quoting of the content is needed.
func (m *Manager) writeNodeFile(ctx context.Context, node, path, content, mode string) error {
	enc := base64.StdEncoding.EncodeToString([]byte(content))
	script := fmt.Sprintf("mkdir -p \"$(dirname %s)\" && echo %s | base64 -d > %s.tmp && chmod %s %s.tmp && mv %s.tmp %s",
		path, enc, path, mode, path, path, path)
	_, err := m.Container.Exec(ctx, node, "sh", "-c", script)
	return err
}

// sshKey returns the cluster's SSH key pair for krunkit nodes, creating it.
func (m *Manager) sshKey(cluster string) (privPath, authorized string, err error) {
	privPath = filepath.Join(m.Store.Dir(cluster), sshKeyFile)
	if b, err := m.Store.ReadSecret(cluster, sshKeyFile+".pub"); err == nil {
		return privPath, strings.TrimSpace(string(b)), nil
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "kona "+cluster)
	if err != nil {
		return "", "", err
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	authorized = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " kona-" + cluster
	if err := m.Store.WriteSecret(cluster, sshKeyFile, pem.EncodeToMemory(block)); err != nil {
		return "", "", err
	}
	return privPath, authorized, m.Store.WriteSecret(cluster, sshKeyFile+".pub", []byte(authorized+"\n"))
}

// GPUSeed renders the cloud-init NoCloud files for a GPU node.
func GPUSeed(c *state.Cluster, n state.Node, authorizedKey, token string, wgConf, wgRoutes string, agentArgs []string) map[string]string {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	env := fmt.Sprintf("K3S_TOKEN=%s\nKONA_K3S_ARGS=%s\n", token, strings.Join(agentArgs, " "))
	return map[string]string{
		"meta-data": fmt.Sprintf("instance-id: %s-%d\nlocal-hostname: %s\n", n.Name, time.Now().Unix(), n.Name),
		"network-config": fmt.Sprintf(`version: 2
ethernets:
  eth0:
    match: {macaddress: "%s"}
    set-name: eth0
    addresses: [%s/%d]
    routes: [{to: default, via: %s}]
    nameservers: {addresses: [%s]}
    mtu: 1500
`, n.MAC, n.IP, c.GPUNetwork.Prefix, c.GPUNetwork.Gateway, c.DNS),
		"user-data": fmt.Sprintf(`#cloud-config
users:
  - name: kona
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    ssh_authorized_keys: [%q]
write_files:
  - {path: /etc/kona/wg0.conf, permissions: '0600', encoding: b64, content: %s}
  - {path: /etc/kona/wg-routes, permissions: '0644', encoding: b64, content: %s}
  - {path: /etc/kona/k3s-agent.env, permissions: '0600', encoding: b64, content: %s}
runcmd:
  - [systemctl, enable, --now, kona-wg.service, kona-agent.service]
`, authorizedKey, b64(wgConf), b64(wgRoutes), b64(env)),
	}
}

// ensureGPUNode creates and boots a krunkit GPU worker (idempotent).
func (m *Manager) ensureGPUNode(ctx context.Context, c *state.Cluster, n state.Node, s distro.Secrets,
	cfgs map[string]wg.Config, gpuImage string) error {
	kn := m.krunkitNode(c, n)
	if m.GPU.Running(kn) {
		m.logf("✓ GPU node %s already running", n.Name)
		return nil
	}
	if _, err := os.Stat(kn.Disk()); errors.Is(err, os.ErrNotExist) {
		if err := krunkit.CloneDisk(gpuImage, kn.Disk()); err != nil {
			return err
		}
	}
	_, authorized, err := m.sshKey(c.Name)
	if err != nil {
		return err
	}
	args, err := k3s.AgentArgs(c, n)
	if err != nil {
		return err
	}
	// The unit runs `k3s agent $KONA_K3S_ARGS`.
	args = append(args[1:], "--node-name", n.Name)
	conf, routes := cfgs[n.Name].Render()
	seed := GPUSeed(c, n, authorized, s[k3s.SecretToken], conf, routes, args)
	if err := krunkit.MakeSeed(ctx, kn.Seed(), seed); err != nil {
		return err
	}
	m.logf("• Starting GPU node %s (krunkit, %s, %d CPU, %s)", n.Name, n.IP, n.CPUs, n.Memory)
	return m.GPU.Start(ctx, kn)
}

// startGPUNodes links the networks and boots every GPU node. CPU nodes get
// their WireGuard config first, so GPU agents reach the API as they boot.
func (m *Manager) startGPUNodes(ctx context.Context, c *state.Cluster, s distro.Secrets, gpuImage string) error {
	if m.GPU == nil {
		return errors.New("GPU nodes need krunkit and vmnet-helper (run `kona doctor --gpu`)")
	}
	cfgs, err := m.wgConfigs(c, s)
	if err != nil {
		return err
	}
	m.logf("• Linking %s and GPU network %s over WireGuard", c.Network.Subnet, c.GPUNetwork.Subnet)
	if err := m.syncCPUWireGuard(ctx, c, cfgs); err != nil {
		return err
	}
	for _, n := range c.GPUNodes() {
		if err := m.ensureGPUNode(ctx, c, n, s, cfgs, gpuImage); err != nil {
			return err
		}
	}
	return nil
}

// SSHArgs returns the ssh command line for a krunkit node.
func (m *Manager) SSHArgs(c *state.Cluster, n state.Node, cmd ...string) []string {
	return append([]string{"-i", filepath.Join(m.Store.Dir(c.Name), sshKeyFile),
		// Host keys are generated on each node's first boot on a host-local
		// vmnet network; they are not pinned.
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR", "-o", "ConnectTimeout=5", "kona@" + n.IP}, cmd...)
}

// VenusDevice returns the Venus device name reported by vulkaninfo, or "".
func VenusDevice(summary string) string {
	var name string
	for _, line := range strings.Split(summary, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "deviceName":
			name = strings.TrimSpace(v)
		case "driverName":
			if strings.TrimSpace(v) == "venus" {
				return name
			}
		}
	}
	return ""
}

// checkVenus labels a Ready GPU node, or untaints it with a loud warning
// when Venus is unavailable so it still serves as a plain worker.
func (m *Manager) checkVenus(ctx context.Context, d distro.Distro, c *state.Cluster, n state.Node) (bool, error) {
	out, err := m.Host.Run(ctx, "ssh", m.SSHArgs(c, n, "vulkaninfo", "--summary")...)
	dev := VenusDevice(string(out))
	cp := c.ControlPlanes()[0].Name
	if dev != "" {
		m.logf("✓ %s: Vulkan via Venus (%s)", n.Name, dev)
		_, err := m.Container.Exec(ctx, cp, d.Kubectl("label", "node", n.Name, "--overwrite", LabelGPU+"="+GPUAvailable)...)
		return err == nil, err
	}
	reason := "no Venus device in vulkaninfo"
	if err != nil {
		reason = lastLine(err)
	}
	m.logf("!!! WARNING: %s has no Venus GPU (%s).\n!!! It joins as an ordinary worker: untainted, label %s=%s, and the DRA plugin will not run there.",
		n.Name, reason, LabelGPU, GPUUnavailable)
	if _, err := m.Container.Exec(ctx, cp, d.Kubectl("taint", "node", n.Name, "kona.dev/gpu-")...); err != nil {
		return false, err
	}
	_, err = m.Container.Exec(ctx, cp, d.Kubectl("label", "node", n.Name, "--overwrite", LabelGPU+"="+GPUUnavailable)...)
	return false, err
}

// planGPU allocates the GPU subnet (once) and adds count GPU workers.
func (m *Manager) planGPU(c *state.Cluster, used []netip.Prefix, count, cpus int, memory string) error {
	if count == 0 {
		return nil
	}
	if c.GPUNetwork.Subnet == "" {
		cs, err := netip.ParsePrefix(c.Network.Subnet)
		if err != nil {
			return err
		}
		sub, err := netplan.PickSubnet(append(used, cs))
		if err != nil {
			return err
		}
		c.GPUNetwork = state.Network{Name: "kona-" + c.Name + "-gpu", Subnet: sub.String(),
			Gateway: netplan.Gateway(sub).String(), Prefix: netplan.Prefix}
	}
	sub := netip.MustParsePrefix(c.GPUNetwork.Subnet)
	start := len(c.GPUNodes())
	for i := start + 1; i <= start+count; i++ {
		ip, err := netplan.GPUWorkerIP(sub, i)
		if err != nil {
			return err
		}
		name := fmt.Sprintf("%s-gpu-worker-%d", c.Name, i)
		c.Nodes = append(c.Nodes, state.Node{Name: name, Role: state.RoleGPUWorker, Kind: state.KindKrunkit,
			IP: ip.String(), MAC: netplan.MAC(c.Name, name), CPUs: cpus, Memory: memory})
	}
	return nil
}

// usedPrefixes returns subnets in use by the host, `container` networks and
// other kona clusters (including their GPU networks, which only exist on the
// host while GPU nodes run).
func (m *Manager) usedPrefixes(ctx context.Context) ([]netip.Prefix, error) {
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
		for _, s := range []string{oc.Network.Subnet, oc.GPUNetwork.Subnet} {
			if p, err := netip.ParsePrefix(s); err == nil {
				used = append(used, p)
			}
		}
	}
	return used, nil
}

// usedWGPorts returns host ports held by other kona clusters.
func (m *Manager) usedWGPorts(except string) (map[int]bool, error) {
	others, err := m.Store.List()
	if err != nil {
		return nil, err
	}
	used := map[int]bool{}
	for _, oc := range others {
		if oc.Name == except {
			continue
		}
		for _, n := range oc.Nodes {
			if n.WGPort != 0 {
				used[n.WGPort] = true
			}
		}
	}
	return used, nil
}
