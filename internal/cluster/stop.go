package cluster

import (
	"context"
	"strings"
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
	return m.StopNodes(ctx, name, nil)
}

// StopNodes stops the named nodes (all when names is empty) in Stop's order.
// Relays are stopped only with the whole cluster: a relay whose CPU node is
// down just has nothing to deliver to.
func (m *Manager) StopNodes(ctx context.Context, name string, names []string) error {
	c, err := m.Store.Load(name)
	if err != nil {
		return err
	}
	want, err := pickNodes(c, names)
	if err != nil {
		return err
	}
	existing, err := m.containersOf(ctx, name)
	if err != nil {
		return err
	}
	stopped := 0
	for _, n := range c.GPUNodes() {
		if m.GPU == nil || !want[n.Name] {
			continue
		}
		if kn := m.krunkitNode(c, n); m.GPU.Running(kn) {
			m.logf("• Stopping GPU node %s", n.Name)
			if err := m.GPU.Stop(ctx, kn); err != nil {
				return err
			}
			stopped++
		}
	}
	if len(names) == 0 {
		if n := m.stopRelays(name); n > 0 {
			m.logf("• Stopped %d WireGuard relay(s)", n)
		}
	}
	for _, controlPlanes := range []bool{false, true} {
		for _, n := range c.CPUNodes() {
			if (n.Role == state.RoleControlPlane) != controlPlanes || !want[n.Name] {
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
		m.logf("✓ %s already stopped", describeNodes(name, names))
		return nil
	}
	if len(names) > 0 {
		m.logf("✓ Stopped %s; start again with: kona start node --name %s %s", strings.Join(names, " "), name, strings.Join(names, " "))
		return nil
	}
	m.logf("✓ Stopped %d node(s) of %q; resume with: kona start cluster %s", stopped, name, name)
	return nil
}
