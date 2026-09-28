package cluster

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/lestex/kona/deploy"
	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/version"
)

// VulkanImage is the Fedora + patched-Mesa userland for Vulkan workloads
// (images/vulkan); imported next to the driver so examples run offline.
func VulkanImage() string { return "ghcr.io/lestex/kona-vulkan:" + version.Get("MESA_VERSION") }

// importImage copies a local `container` image into a GPU node's k3s
// containerd over SSH. (Phase 4 replaces this with the kona-registry.)
func (m *Manager) importImage(ctx context.Context, c *state.Cluster, n state.Node, image string) error {
	tmp, err := os.CreateTemp(RunDir(c.Name), ".image-*.tar")
	if err != nil {
		if err := os.MkdirAll(RunDir(c.Name), 0o700); err != nil {
			return err
		}
		if tmp, err = os.CreateTemp(RunDir(c.Name), ".image-*.tar"); err != nil {
			return err
		}
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if out, err := exec.CommandContext(ctx, "container", "image", "save", "--platform", "linux/arm64",
		"-o", tmp.Name(), image).CombinedOutput(); err != nil {
		return fmt.Errorf("container image save %s: %v: %s (build it with `make -C dra/kona-venus-dra images`)", image, err, out)
	}
	f, err := os.Open(tmp.Name())
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, "ssh", m.SSHArgs(c, n, "sudo k3s ctr -n k8s.io images import --platform linux/arm64 -")...)
	cmd.Stdin = f
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("import %s into %s: %v: %s", image, n.Name, err, out)
	}
	return nil
}

// deployVenusDRA installs kona-venus-dra and the kona-gpu DeviceClass for
// the given Venus-capable GPU nodes (idempotent).
func (m *Manager) deployVenusDRA(ctx context.Context, d distro.Distro, c *state.Cluster, nodes []state.Node) error {
	if len(nodes) == 0 {
		return nil
	}
	importImage := m.ImportImage
	if importImage == nil {
		importImage = m.importImage
	}
	for _, n := range nodes {
		for _, img := range []string{deploy.VenusDRAImage, VulkanImage()} {
			m.logf("• Loading %s into %s", img, n.Name)
			if err := importImage(ctx, c, n, img); err != nil {
				return err
			}
		}
	}
	cp := c.ControlPlanes()[0].Name
	path := filepath.Join("/etc/kona", "kona-venus-dra.yaml")
	if err := m.writeNodeFile(ctx, cp, path, string(deploy.VenusDRA), "0644"); err != nil {
		return err
	}
	m.logf("• Deploying kona-venus-dra (DRA driver) and DeviceClass kona-gpu")
	if _, err := m.Container.Exec(ctx, cp, d.Kubectl("apply", "-f", path)...); err != nil {
		return fmt.Errorf("apply kona-venus-dra: %w", err)
	}
	if _, err := m.Container.Exec(ctx, cp, d.Kubectl("-n", "kona-system", "rollout", "status",
		"daemonset/kona-venus-dra", "--timeout=180s")...); err != nil {
		return fmt.Errorf("kona-venus-dra did not become ready: %s (see `kubectl -n kona-system logs ds/kona-venus-dra`)", lastLine(err))
	}
	m.logf("✓ kona-venus-dra is running on %d GPU node(s)", len(nodes))
	return nil
}
