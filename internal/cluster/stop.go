package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lestex/kona/internal/runtime/container"
	"github.com/lestex/kona/internal/state"
)

// StopGrace is how long a node's init gets to shut k3s/kubelet down before
// it is killed.
const StopGrace = 30 * time.Second

// Stop shuts every running node of a cluster down, keeping VMs, disks and
// state: workers first, then control planes, so agents never outlive the
// API. `kona start cluster` resumes it.
func (m *Manager) Stop(ctx context.Context, name string) error {
	return m.StopNodes(ctx, name, nil)
}

// StopNodes stops the named nodes (all when names is empty) in Stop's order.
func (m *Manager) StopNodes(ctx context.Context, name string, names []string) error {
	c, err := m.Store.Load(name)
	if err != nil {
		return err
	}
	if err := supported(c); err != nil {
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
	if len(names) > 0 {
		if err := keepsQuorum(c, want, existing); err != nil {
			return err
		}
	}
	stopped := 0
	for _, controlPlanes := range []bool{false, true} {
		for _, n := range c.Nodes {
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
	// The registry goes last, with the whole cluster only.
	if r := c.Registry; r != nil && len(names) == 0 {
		if ct, ok := existing[r.Name]; ok && ct.Status.State == "running" {
			m.logf("• Stopping %s", r.Name)
			if err := m.Container.Stop(ctx, r.Name, StopGrace); err != nil {
				return err
			}
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

// keepsQuorum refuses a `stop node` that would leave fewer running control
// planes than etcd quorum (a majority; 1 of 1 for a single control plane):
// the API would go down with them. Stopping the whole cluster is the way to
// take the API down on purpose.
func keepsQuorum(c *state.Cluster, want map[string]bool, existing map[string]container.Container) error {
	cps := c.ControlPlanes()
	left := 0
	var named []string
	for _, n := range cps {
		if want[n.Name] {
			named = append(named, n.Name)
			continue
		}
		if ct, ok := existing[n.Name]; ok && ct.Status.State == "running" {
			left++
		}
	}
	quorum := len(cps)/2 + 1
	if len(named) == 0 || left >= quorum {
		return nil
	}
	return fmt.Errorf("stopping %s would leave %d of %d control planes running (the API needs %d); "+
		"stop the whole cluster with `kona stop cluster %s`", strings.Join(named, ", "), left, len(cps), quorum, c.Name)
}
