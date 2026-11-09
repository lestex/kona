package cluster

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/netplan"
	"github.com/lestex/kona/internal/runtime/container"
	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/version"
)

const (
	// RegistryPort is where kona-registry listens (plain HTTP).
	RegistryPort       = 5000
	registryVolumeSize = "20G"
	registryDataDir    = "/var/lib/registry"
	// LabelRoleRegistry marks the registry VM (label kona.role).
	LabelRoleRegistry = "registry"
)

// RegistryImage is the pinned distribution image kona-registry runs.
func RegistryImage() string {
	img := "docker.io/library/registry:" + version.Get("REGISTRY_VERSION")
	if sum := version.Checksum("REGISTRY_IMAGE_SHA256"); sum != "" {
		img += "@sha256:" + sum
	}
	return img
}

// RegistryName is the registry VM of a cluster: kona-registry for the
// default cluster.
func RegistryName(cluster string) string { return cluster + "-registry" }

// ImageRef is a parsed image reference.
type ImageRef struct {
	Host string // registry host, e.g. docker.io or ghcr.io
	Repo string // repository path on that host, e.g. library/nginx
	Tag  string
}

// ParseImageRef normalizes ref the way containerd does: no host means
// docker.io, and a one-part docker.io name lives under library/. The tag
// defaults to latest; digest references are not supported.
func ParseImageRef(ref string) (ImageRef, error) {
	if ref == "" || strings.Contains(ref, "@") {
		return ImageRef{}, fmt.Errorf("image %q: use a name with a tag (digest references are not supported)", ref)
	}
	r := ImageRef{Host: "docker.io", Repo: ref, Tag: "latest"}
	if first, rest, ok := strings.Cut(ref, "/"); ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		r.Host, r.Repo = first, rest
	}
	if i := strings.LastIndex(r.Repo, ":"); i > strings.LastIndex(r.Repo, "/") {
		r.Repo, r.Tag = r.Repo[:i], r.Repo[i+1:]
	}
	if r.Host == "index.docker.io" || r.Host == "registry-1.docker.io" {
		r.Host = "docker.io"
	}
	if r.Host == "docker.io" && !strings.Contains(r.Repo, "/") {
		r.Repo = "library/" + r.Repo
	}
	if r.Repo == "" || r.Tag == "" {
		return ImageRef{}, fmt.Errorf("invalid image reference %q", ref)
	}
	return r, nil
}

// String is the fully qualified reference.
func (r ImageRef) String() string { return r.Host + "/" + r.Repo + ":" + r.Tag }

// upstream is the URL containerd falls back to for host.
func upstream(host string) string {
	if host == "docker.io" {
		return "https://registry-1.docker.io"
	}
	return "https://" + host
}

// MirrorHostsTOML is a node's containerd config for one upstream registry:
// try kona-registry first, where kona stores that host's images under
// /v2/<host>/..., then the upstream itself.
func MirrorHostsTOML(host, registryIP string) string {
	return fmt.Sprintf(`# Written by kona: images loaded with `+"`kona load image`"+` come from kona-registry.
server = %q

[host."http://%s:%d/v2/%s"]
  capabilities = ["pull", "resolve"]
  override_path = true
`, upstream(host), registryIP, RegistryPort, host)
}

// LoadImages pushes local `container` images into the cluster's
// kona-registry (creating it on first use) and points every node's
// containerd at it for those images' registries. Pods then pull the images
// by their usual names.
func (m *Manager) LoadImages(ctx context.Context, name string, images []string) error {
	c, err := m.Store.Load(name)
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
	refs := make([]ImageRef, len(images))
	for i, img := range images {
		if refs[i], err = ParseImageRef(img); err != nil {
			return err
		}
		if !m.Container.ImageExists(ctx, img) {
			return fmt.Errorf("image %s is not in the local `container` image store (pull or build it first)", img)
		}
	}
	if err := m.ensureRegistry(ctx, c); err != nil {
		return err
	}
	for i, img := range images {
		dst := fmt.Sprintf("%s:%d/%s/%s:%s", c.Registry.IP, RegistryPort, refs[i].Host, refs[i].Repo, refs[i].Tag)
		m.logf("• Loading %s into %s", refs[i], c.Registry.Name)
		if err := m.Container.TagImage(ctx, img, dst); err != nil {
			return err
		}
		perr := m.Container.PushImage(ctx, dst)
		// The registry-qualified name is only a push handle.
		if err := m.Container.RemoveImage(ctx, dst); err != nil && perr == nil {
			perr = err
		}
		if perr != nil {
			return fmt.Errorf("push %s: %w", img, perr)
		}
		if !slices.Contains(c.Registry.Hosts, refs[i].Host) {
			c.Registry.Hosts = append(c.Registry.Hosts, refs[i].Host)
			slices.Sort(c.Registry.Hosts)
		}
	}
	if err := m.Store.Save(c); err != nil {
		return err
	}
	for _, n := range c.Nodes {
		if err := m.writeMirrors(ctx, d, c, n); err != nil {
			return err
		}
	}
	m.logf("✓ Loaded %d image(s); nodes pull them from %s (%s)", len(images), c.Registry.Name, c.Registry.IP)
	return nil
}

// writeMirrors writes the cluster's mirror config into one node.
func (m *Manager) writeMirrors(ctx context.Context, d distro.Distro, c *state.Cluster, n state.Node) error {
	if c.Registry == nil {
		return nil
	}
	for _, h := range c.Registry.Hosts {
		p := path.Join(d.RegistryHostsDir(), h, "hosts.toml")
		if err := m.writeNodeFile(ctx, n.Name, p, MirrorHostsTOML(h, c.Registry.IP), "0644"); err != nil {
			return fmt.Errorf("mirror config on %s: %w", n.Name, err)
		}
	}
	return nil
}

// writeNodeFile writes content into a node via base64, so no shell quoting
// of the content is needed.
func (m *Manager) writeNodeFile(ctx context.Context, node, file, content, mode string) error {
	enc := base64Encode(content)
	script := fmt.Sprintf(`mkdir -p "$(dirname %[1]s)" && echo %[2]s | base64 -d > %[1]s.tmp && chmod %[3]s %[1]s.tmp && mv %[1]s.tmp %[1]s`,
		file, enc, mode)
	_, err := m.Container.Exec(ctx, node, "sh", "-c", script)
	return err
}

// ensureRegistry creates or starts the cluster's registry VM and waits for
// its API.
func (m *Manager) ensureRegistry(ctx context.Context, c *state.Cluster) error {
	if c.Registry == nil {
		subnet, err := netip.ParsePrefix(c.Network.Subnet)
		if err != nil {
			return err
		}
		name := RegistryName(c.Name)
		c.Registry = &state.Registry{Name: name, IP: netplan.RegistryIP(subnet).String(),
			MAC: netplan.MAC(c.Name, name), Volume: name + "-data"}
		if err := m.Store.Save(c); err != nil {
			return err
		}
	}
	r := c.Registry
	existing, err := m.containersOf(ctx, c.Name)
	if err != nil {
		return err
	}
	ct, ok := existing[r.Name]
	switch {
	case ok && ct.Status.State == "running":
	case ok:
		m.logf("• Starting %s", r.Name)
		if err := m.Container.Start(ctx, r.Name); err != nil {
			return err
		}
	default:
		labels := map[string]string{container.LabelCluster: c.Name, container.LabelRole: LabelRoleRegistry}
		if err := m.ensureVolume(ctx, r.Volume, registryVolumeSize, labels); err != nil {
			return err
		}
		m.logf("• Starting %s (%s)", r.Name, r.IP)
		// `container` has no static IP option and its address can be a
		// node's static IP, so the registry swaps it for kona's address, as
		// node entrypoints do (docs/networking.md).
		if err := m.Container.Run(ctx, container.RunSpec{
			Name: r.Name, Image: RegistryImage(), Network: c.Network.Name, MAC: r.MAC, MTU: nodeMTU,
			CPUs: 1, Memory: "512M", Labels: labels, Volumes: map[string]string{r.Volume: registryDataDir},
			Entrypoint: "sh",
			Args: []string{"-c", fmt.Sprintf("gw=$(ip -4 route show default | awk '{print $3; exit}') && "+
				"ip -4 addr flush dev eth0 && ip addr add %s/%d dev eth0 && ip route add default via \"$gw\" && "+
				"exec registry serve /etc/distribution/config.yml", r.IP, c.Network.Prefix)},
		}); err != nil {
			return err
		}
	}
	return m.waitRegistry(ctx, r)
}

func (m *Manager) waitRegistry(ctx context.Context, r *state.Registry) error {
	probe := m.RegistryProbe
	if probe == nil {
		probe = registryUp
	}
	deadline := m.Now().Add(time.Minute)
	var last error
	for m.Now().Before(deadline) {
		if last = probe(r.IP); last == nil {
			return nil
		}
		if err := m.Sleep(ctx, pollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("%s did not answer on %s:%d: %v (see `container logs %s`)", r.Name, r.IP, RegistryPort, last, r.Name)
}

// registryUp checks the registry API from the host.
func registryUp(ip string) error {
	cl := http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get("http://" + net.JoinHostPort(ip, fmt.Sprint(RegistryPort)) + "/v2/")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}

func base64Encode(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
