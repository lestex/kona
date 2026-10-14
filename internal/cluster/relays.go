package cluster

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/lestex/kona/internal/procx"
	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/wg"
)

// relayTag marks relay processes of one cluster in their command line, so
// Delete can find them even without state.
func relayTag(cluster string) string { return "--cluster=" + cluster }

// RelayArgs is the `kona wg-relay` command line for a CPU node.
func RelayArgs(cluster string, n state.Node) []string {
	return []string{"wg-relay", relayTag(cluster), "--node=" + n.Name,
		"--listen=0.0.0.0:" + strconv.Itoa(n.WGPort),
		fmt.Sprintf("--target=%s:%d", n.IP, wg.ListenPort)}
}

// ensureRelays runs one host UDP relay per CPU node, carrying WireGuard from
// GPU nodes (which dial <gpu gateway>:<WGPort>) to the node's static IP.
func (m *Manager) ensureRelays(c *state.Cluster) error {
	if c.GPULink() != state.GPULinkRelay {
		return nil
	}
	start := m.StartRelay
	if start == nil {
		start = m.startRelay
	}
	for _, n := range c.CPUNodes() {
		if n.WGPort == 0 {
			continue
		}
		if err := start(c, n); err != nil {
			return fmt.Errorf("WireGuard relay for %s: %w", n.Name, err)
		}
	}
	return nil
}

func (m *Manager) startRelay(c *state.Cluster, n state.Node) error {
	dir := m.nodeDir(c.Name, n.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	pidfile := filepath.Join(dir, "relay.pid")
	if pid, ok := procx.ReadPid(pidfile); ok && procx.Alive(pid, relayTag(c.Name)) {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	log := filepath.Join(dir, "relay.log")
	pid, err := procx.Spawn(log, log, exe, RelayArgs(c.Name, n)...)
	if err != nil {
		return err
	}
	return procx.WritePid(pidfile, pid)
}

// stopRelays kills a cluster's relays, with or without state.
func (m *Manager) stopRelays(cluster string) int {
	return len(m.killMatching("wg-relay " + relayTag(cluster) + " "))
}
