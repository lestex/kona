package cluster

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/lestex/kona/internal/netplan"
	"github.com/lestex/kona/internal/state"
)

// AddOptions configure `kona add node`.
type AddOptions struct {
	Cluster string
	CPUs    int
	Memory  string
	Wait    time.Duration
}

// AddNode adds one worker to a ready cluster.
func (m *Manager) AddNode(ctx context.Context, o AddOptions) error {
	c, err := m.Store.Load(o.Cluster)
	if err != nil {
		return err
	}
	if err := supported(c); err != nil {
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
	n, err := m.planWorker(c, o)
	if err != nil {
		return err
	}
	if err := m.Store.Save(c); err != nil {
		return err
	}
	existing, err := m.containersOf(ctx, c.Name)
	if err != nil {
		return err
	}
	if err := m.ensureNode(ctx, d, c, n, s, existing); err != nil {
		return err
	}
	m.logf("• Waiting up to %s for %s to be Ready", deadline.Sub(m.Now()).Round(time.Second), n.Name)
	if err := m.waitNodesReady(ctx, d, c, []string{n.Name}, deadline, false); err != nil {
		return err
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
	name := fmt.Sprintf("%s-worker-%d", c.Name, i)
	n := state.Node{Name: name, Role: state.RoleWorker, Kind: state.KindContainer, IP: ip.String(),
		MAC: netplan.MAC(c.Name, name), CPUs: o.CPUs, Memory: o.Memory}
	c.Nodes = append(c.Nodes, n)
	return n, nil
}
