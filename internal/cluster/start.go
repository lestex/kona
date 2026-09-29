package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/lestex/kona/internal/state"
)

// Start boots every stopped node of a cluster: `container` VMs after a host
// reboot, krunkit GPU VMs that did not survive a reboot or host sleep. Node
// configuration lives on the node disks (static IP, WireGuard, k3s env), so
// the entrypoint and systemd units reapply it on boot.
func (m *Manager) Start(ctx context.Context, name string, wait time.Duration) error {
	c, err := m.Store.Load(name)
	if err != nil {
		return err
	}
	d, err := DistroFor(c.Distro)
	if err != nil {
		return err
	}
	existing, err := m.containersOf(ctx, name)
	if err != nil {
		return err
	}
	deadline := m.Now().Add(wait)
	started := 0
	// Control planes first: agents need the API.
	for _, controlPlanes := range []bool{true, false} {
		for _, n := range c.Nodes {
			if (n.Role == state.RoleControlPlane) != controlPlanes {
				continue
			}
			switch n.Kind {
			case state.KindContainer:
				ct, ok := existing[n.Name]
				if !ok {
					return fmt.Errorf("node %s has no VM; recreate the cluster (kona delete cluster %s && kona create cluster %s)", n.Name, name, name)
				}
				if ct.Status.State == "running" {
					continue
				}
				m.logf("• Starting %s", n.Name)
				if err := m.Container.Start(ctx, n.Name); err != nil {
					return err
				}
			case state.KindKrunkit:
				if m.GPU == nil {
					return fmt.Errorf("GPU node %s needs krunkit and vmnet-helper (run `kona doctor --gpu`)", n.Name)
				}
				kn := m.krunkitNode(c, n)
				if m.GPU.Running(kn) {
					continue
				}
				m.logf("• Starting GPU node %s", n.Name)
				if err := m.GPU.Start(ctx, kn); err != nil {
					return err
				}
			}
			started++
		}
	}
	if started == 0 {
		m.logf("✓ All nodes of %q are already running", name)
		return nil
	}
	if err := m.waitAPI(ctx, d, c, deadline); err != nil {
		return err
	}
	m.logf("• Waiting up to %s for %d nodes to be Ready", deadline.Sub(m.Now()).Round(time.Second), len(c.Nodes))
	return m.waitNodesReady(ctx, d, c, nodeNames(c.Nodes), deadline, true)
}
