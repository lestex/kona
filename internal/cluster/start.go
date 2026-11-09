package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lestex/kona/internal/state"
)

// Start boots every stopped node of a cluster, for example after a host
// reboot. Node configuration lives in the VMs (static IP, k3s env), so the
// entrypoint reapplies it on boot.
func (m *Manager) Start(ctx context.Context, name string, wait time.Duration) error {
	return m.StartNodes(ctx, name, nil, wait)
}

// StartNodes boots the named stopped nodes (all when names is empty), in
// the same order as Start, and waits for them to be Ready again.
func (m *Manager) StartNodes(ctx context.Context, name string, names []string, wait time.Duration) error {
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
	d, err := DistroFor(c.Distro)
	if err != nil {
		return err
	}
	existing, err := m.containersOf(ctx, name)
	if err != nil {
		return err
	}
	deadline := m.Now().Add(wait)
	rebooted := map[string]bool{}
	// Control planes first: agents need the API.
	for _, controlPlanes := range []bool{true, false} {
		for _, n := range c.Nodes {
			if (n.Role == state.RoleControlPlane) != controlPlanes || !want[n.Name] {
				continue
			}
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
			rebooted[n.Name] = true
		}
	}
	if len(rebooted) == 0 {
		m.logf("✓ %s already running", describeNodes(name, names))
		return nil
	}
	if err := m.waitAPI(ctx, d, c, deadline); err != nil {
		return err
	}
	wait4 := nodeNames(c.Nodes)
	if len(names) > 0 {
		wait4 = names
	}
	m.logf("• Waiting up to %s for %d nodes to be Ready", deadline.Sub(m.Now()).Round(time.Second), len(wait4))
	if err := m.waitNodesFresh(ctx, d, c, wait4, rebooted, deadline, true); err != nil {
		return err
	}
	return m.waitHealthy(ctx, name, deadline)
}

// waitHealthy polls Verify until no check fails: a node turns Ready before
// its CNI and DNS pods are back, and the cluster isn't usable until
// they are.
func (m *Manager) waitHealthy(ctx context.Context, name string, deadline time.Time) error {
	m.logf("• Waiting for system pods and DNS")
	var failed []string
	for m.Now().Before(deadline) {
		r, err := m.Verify(ctx, name)
		failed = failed[:0]
		if err != nil {
			failed = append(failed, err.Error())
		}
		for _, c := range r.Checks {
			if c.Status == CheckFail {
				failed = append(failed, c.ID+": "+c.Detail)
			}
		}
		if len(failed) == 0 {
			m.logf("✓ Cluster %q is healthy", name)
			return nil
		}
		if err := m.Sleep(ctx, pollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("cluster not healthy before --wait expired (see `kona verify cluster %s`): %s", name, strings.Join(failed, "; "))
}

// pickNodes returns the set of node names to act on: names, validated, or
// every node when names is empty.
func pickNodes(c *state.Cluster, names []string) (map[string]bool, error) {
	want := map[string]bool{}
	for _, n := range c.Nodes {
		want[n.Name] = len(names) == 0
	}
	for _, n := range names {
		if _, ok := want[n]; !ok {
			return nil, fmt.Errorf("cluster %q has no node %q (see: kona get nodes --name %s)", c.Name, n, c.Name)
		}
		want[n] = true
	}
	return want, nil
}

func describeNodes(cluster string, names []string) string {
	if len(names) == 0 {
		return fmt.Sprintf("All nodes of %q are", cluster)
	}
	return strings.Join(names, ", ") + " " + map[bool]string{true: "is", false: "are"}[len(names) == 1]
}
