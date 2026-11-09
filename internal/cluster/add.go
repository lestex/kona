package cluster

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	"github.com/lestex/kona/internal/cni"
	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/kubeconfig"
	"github.com/lestex/kona/internal/netplan"
	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/tools"
	"github.com/lestex/kona/internal/wg"
)

// AddOptions configure `kona add node`.
type AddOptions struct {
	Cluster  string
	GPU      bool
	CPUs     int
	Memory   string
	GPUImage string
	Wait     time.Duration
}

// AddNode adds one worker (CPU, or GPU with o.GPU) to a ready cluster and
// relinks WireGuard so CPU and GPU networks keep reaching each other.
func (m *Manager) AddNode(ctx context.Context, o AddOptions) error {
	c, err := m.Store.Load(o.Cluster)
	if err != nil {
		return err
	}
	if c.Phase != state.PhaseReady {
		return fmt.Errorf("cluster %q is %s; finish it with `kona create cluster %s` first", c.Name, c.Phase, c.Name)
	}
	d, err := DistroFor(c.Distro)
	if err != nil {
		return err
	}
	if _, err := ParseSize(o.Memory); err != nil {
		return err
	}
	s, err := m.secrets(c.Name, d)
	if err != nil {
		return err
	}
	deadline := m.Now().Add(o.Wait)
	var n state.Node
	firstGPU := len(c.GPUNodes()) == 0
	if o.GPU {
		if c.Distro != "k3s" {
			return fmt.Errorf("GPU nodes are only implemented for --distro k3s")
		}
		used, err := m.usedPrefixes(ctx)
		if err != nil {
			return err
		}
		if err := m.planGPU(c, used, 1, o.CPUs, o.Memory); err != nil {
			return err
		}
		n = c.GPUNodes()[len(c.GPUNodes())-1]
	} else {
		if n, err = m.planWorker(c, o); err != nil {
			return err
		}
	}
	if err := m.Store.Save(c); err != nil {
		return err
	}

	if o.GPU {
		if c.CNI == "cilium" && firstGPU {
			if err := m.lowerCiliumMTU(ctx, c); err != nil {
				return err
			}
		}
		if err := m.startGPUNodes(ctx, c, s, o.GPUImage); err != nil {
			return err
		}
	} else {
		existing, err := m.containersOf(ctx, c.Name)
		if err != nil {
			return err
		}
		if err := m.ensureNode(ctx, d, c, n, s, existing); err != nil {
			return err
		}
		if len(c.GPUNodes()) > 0 {
			if err := m.ensureRelays(c); err != nil {
				return err
			}
			if err := m.relinkAll(ctx, c, s); err != nil {
				return err
			}
		}
	}
	m.logf("• Waiting up to %s for %s to be Ready", deadline.Sub(m.Now()).Round(time.Second), n.Name)
	if err := m.waitNodesReady(ctx, d, c, []string{n.Name}, deadline, false); err != nil {
		return err
	}
	if o.GPU {
		ok, err := m.checkVenus(ctx, d, c, n)
		if err != nil {
			return err
		}
		if ok {
			if err := m.deployVenusDRA(ctx, d, c, []state.Node{n}); err != nil {
				return err
			}
		}
	}
	m.logf("✓ Node %s (%s) joined cluster %q", n.Name, n.IP, c.Name)
	return nil
}

func (m *Manager) planWorker(c *state.Cluster, o AddOptions) (state.Node, error) {
	subnet, err := netip.ParsePrefix(c.Network.Subnet)
	if err != nil {
		return state.Node{}, err
	}
	i := 1
	for _, n := range c.Nodes {
		if n.Role == state.RoleWorker {
			i++
		}
	}
	ip, err := netplan.WorkerIP(subnet, i)
	if err != nil {
		return state.Node{}, err
	}
	taken, err := m.usedWGPorts("")
	if err != nil {
		return state.Node{}, err
	}
	ports, err := netplan.PickPorts(1, taken, m.PortFree)
	if err != nil {
		return state.Node{}, err
	}
	name := fmt.Sprintf("%s-worker-%d", c.Name, i)
	n := state.Node{Name: name, Role: state.RoleWorker, Kind: state.KindContainer, IP: ip.String(),
		MAC: netplan.MAC(c.Name, name), CPUs: o.CPUs, Memory: o.Memory, WGPort: ports[0]}
	c.Nodes = append(c.Nodes, n)
	return n, nil
}

// relinkAll pushes WireGuard configs to every CPU node and every GPU node
// (a new CPU node is a new peer and endpoint for the GPU side).
func (m *Manager) relinkAll(ctx context.Context, c *state.Cluster, s distro.Secrets) error {
	cfgs, err := m.wgConfigs(c, s)
	if err != nil {
		return err
	}
	if err := m.syncCPUWireGuard(ctx, c, cfgs); err != nil {
		return err
	}
	for _, g := range c.GPUNodes() {
		if err := m.syncGPUWireGuard(ctx, c, g, cfgs[g.Name]); err != nil {
			return err
		}
	}
	return nil
}

// syncGPUWireGuard updates a running GPU node over SSH.
func (m *Manager) syncGPUWireGuard(ctx context.Context, c *state.Cluster, n state.Node, cfg wg.Config) error {
	conf, routes := cfg.Render()
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	script := fmt.Sprintf(
		"echo %s | base64 -d | sudo install -m 0600 /dev/stdin /etc/kona/wg0.conf && "+
			"echo %s | base64 -d | sudo install -m 0644 /dev/stdin /etc/kona/wg-routes && sudo /usr/local/bin/kona-wg",
		b64(conf), b64(routes))
	if _, err := m.Host.Run(ctx, "ssh", m.SSHArgs(c, n, script)...); err != nil {
		return fmt.Errorf("update WireGuard on %s: %w", n.Name, err)
	}
	return nil
}

// lowerCiliumMTU makes room for WireGuard under VXLAN once a cluster gets
// its first GPU node.
func (m *Manager) lowerCiliumMTU(ctx context.Context, c *state.Cluster) error {
	kc, err := kubeconfig.Path(c.Name)
	if err != nil {
		return err
	}
	cli, err := m.Tools.Ensure(ctx, tools.CiliumCLI())
	if err != nil {
		return err
	}
	m.logf("• Setting Cilium's underlay MTU to %d for the WireGuard link (pods started earlier keep their MTU until restarted)", cni.GPUUnderlayMTU)
	_, err = m.ciliumRunner().Run(ctx, cli, "upgrade", "--kubeconfig", kc, "--context", kubeconfig.ContextName(c.Name),
		"--version", cni.CiliumVersion(), "--reuse-values", "--set", "MTU="+strconv.Itoa(cni.GPUUnderlayMTU))
	return err
}
