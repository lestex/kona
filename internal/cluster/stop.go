package cluster

import (
	"context"
	"time"

	"github.com/lestex/kona/internal/state"
)

// StopGrace is how long a node's init gets to shut k3s/kubelet down before
// it is killed.
const StopGrace = 30 * time.Second

// Stop shuts every running node of a cluster down, keeping VMs, disks and
// state: GPU nodes and their relays first, then workers, then control
// planes, so agents never outlive the API. `kona start cluster` resumes it.
func (m *Manager) Stop(ctx context.Context, name string) error {
	c, err := m.Store.Load(name)
	if err != nil {
		return err
	}
	existing, err := m.containersOf(ctx, name)
	if err != nil {
		return err
	}
	stopped := 0
	for _, n := range c.GPUNodes() {
		if m.GPU == nil {
			break
		}
		if kn := m.krunkitNode(c, n); m.GPU.Running(kn) {
			m.logf("• Stopping GPU node %s", n.Name)
			if err := m.GPU.Stop(ctx, kn); err != nil {
				return err
			}
			stopped++
		}
	}
	if n := m.stopRelays(name); n > 0 {
		m.logf("• Stopped %d WireGuard relay(s)", n)
	}
	for _, controlPlanes := range []bool{false, true} {
		for _, n := range c.CPUNodes() {
			if (n.Role == state.RoleControlPlane) != controlPlanes {
				continue
			}
			if ct, ok := existing[n.Name]; !ok || ct.Status.State != "running" {
				continue
			}
			m.logf("• Stopping %s", n.Name)
			if err := m.Container.Stop(ctx, n.Name, StopGrace); err != nil {
				return err
			}
			stopped++
		}
	}
	if stopped == 0 {
		m.logf("✓ All nodes of %q are already stopped", name)
		return nil
	}
	m.logf("✓ Stopped %d node(s) of %q; resume with: kona start cluster %s", stopped, name, name)
	return nil
}
