// Package container drives Apple's `container` CLI. Flags used here are
// checked against docs/cli-surface.md.
package container

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lestex/kona/internal/execx"
)

// Label keys kona puts on every resource it creates.
const (
	LabelCluster = "kona.cluster"
	LabelRole    = "kona.role"
)

// Client wraps the `container` binary.
type Client struct {
	Bin    string
	Runner execx.Runner
}

// New returns a client for the `container` binary on PATH.
func New(r execx.Runner) *Client {
	return &Client{Bin: "container", Runner: r}
}

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	return c.Runner.Run(ctx, c.Bin, args...)
}

// IsNotFound reports whether err is `container`'s "not found" error.
func IsNotFound(err error) bool {
	var e *execx.Error
	if !errors.As(err, &e) {
		return false
	}
	s := strings.ToLower(e.Stderr)
	return strings.Contains(s, "notfound") || strings.Contains(s, "not found") ||
		strings.Contains(s, "failed to delete one or more")
}

// Network is the subset of `container network ls --format json` kona uses.
type Network struct {
	ID            string `json:"id"`
	Configuration struct {
		Labels     map[string]string `json:"labels"`
		IPv4Subnet string            `json:"ipv4Subnet"`
	} `json:"configuration"`
	Status struct {
		IPv4Gateway string `json:"ipv4Gateway"`
		IPv4Subnet  string `json:"ipv4Subnet"`
	} `json:"status"`
}

// Subnet returns the network's IPv4 subnet.
func (n Network) Subnet() string {
	if n.Status.IPv4Subnet != "" {
		return n.Status.IPv4Subnet
	}
	return n.Configuration.IPv4Subnet
}

// Networks lists all networks.
func (c *Client) Networks(ctx context.Context) ([]Network, error) {
	out, err := c.run(ctx, "network", "ls", "--format", "json")
	if err != nil {
		return nil, err
	}
	var ns []Network
	if err := json.Unmarshal(out, &ns); err != nil {
		return nil, fmt.Errorf("parse network list: %w", err)
	}
	return ns, nil
}

// CreateNetwork creates a NAT network with a fixed subnet.
func (c *Client) CreateNetwork(ctx context.Context, name, subnet string, labels map[string]string) error {
	args := []string{"network", "create", "--subnet", subnet}
	args = append(args, labelArgs(labels)...)
	args = append(args, name)
	_, err := c.run(ctx, args...)
	return err
}

// DeleteNetwork deletes a network. Missing networks are not an error.
func (c *Client) DeleteNetwork(ctx context.Context, name string) error {
	_, err := c.run(ctx, "network", "rm", name)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// Volume is the subset of `container volume ls --format json` kona uses.
type Volume struct {
	ID            string `json:"id"`
	Configuration struct {
		Labels map[string]string `json:"labels"`
		Source string            `json:"source"`
	} `json:"configuration"`
}

// Volumes lists all volumes.
func (c *Client) Volumes(ctx context.Context) ([]Volume, error) {
	out, err := c.run(ctx, "volume", "ls", "--format", "json")
	if err != nil {
		return nil, err
	}
	var vs []Volume
	if err := json.Unmarshal(out, &vs); err != nil {
		return nil, fmt.Errorf("parse volume list: %w", err)
	}
	return vs, nil
}

// CreateVolume creates a block-backed (ext4 image) volume.
func (c *Client) CreateVolume(ctx context.Context, name, size string, labels map[string]string) error {
	args := []string{"volume", "create", "-s", size}
	args = append(args, labelArgs(labels)...)
	args = append(args, name)
	_, err := c.run(ctx, args...)
	return err
}

// DeleteVolume deletes a volume. Missing volumes are not an error.
func (c *Client) DeleteVolume(ctx context.Context, name string) error {
	_, err := c.run(ctx, "volume", "rm", name)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// RunSpec describes one node VM.
type RunSpec struct {
	Name    string
	Init    bool // run `container`'s init as PID 1 (reaps zombies)
	Tmpfs   []string
	Image   string
	Kernel  string
	Network string
	MAC     string
	MTU     int
	DNS     string
	CPUs    int
	Memory  string
	Env     map[string]string
	Labels  map[string]string
	Volumes map[string]string // volume name -> mount path
	Publish []string          // [host-ip:]host-port:container-port[/proto]
	Args    []string          // arguments to the image entrypoint
}

// RunArgs returns the `container run` arguments for spec.
func RunArgs(s RunSpec) []string {
	args := []string{"run", "-d", "--progress", "none", "--name", s.Name,
		// Nodes run containerd and the kubelet: full caps and no masked or
		// read-only /proc and /sys paths (same as `container k8s`).
		"--cap-add", "ALL", "--masked-path", "NONE", "--read-only-path", "NONE"}
	if s.Init {
		args = append(args, "--init")
	}
	for _, t := range s.Tmpfs {
		args = append(args, "--tmpfs", t)
	}
	if s.Kernel != "" {
		args = append(args, "--kernel", s.Kernel)
	}
	nw := s.Network
	if s.MAC != "" {
		nw += ",mac=" + s.MAC
	}
	if s.MTU > 0 {
		nw += fmt.Sprintf(",mtu=%d", s.MTU)
	}
	if nw != "" {
		args = append(args, "--network", nw)
	}
	if s.DNS != "" {
		args = append(args, "--dns", s.DNS)
	}
	if s.CPUs > 0 {
		args = append(args, "--cpus", fmt.Sprint(s.CPUs))
	}
	if s.Memory != "" {
		args = append(args, "--memory", s.Memory)
	}
	for _, k := range sortedKeys(s.Env) {
		args = append(args, "-e", k+"="+s.Env[k])
	}
	args = append(args, labelArgs(s.Labels)...)
	for _, k := range sortedKeys(s.Volumes) {
		args = append(args, "-v", k+":"+s.Volumes[k])
	}
	for _, p := range s.Publish {
		args = append(args, "-p", p)
	}
	args = append(args, s.Image)
	return append(args, s.Args...)
}

// Run starts a detached node VM.
func (c *Client) Run(ctx context.Context, s RunSpec) error {
	_, err := c.run(ctx, RunArgs(s)...)
	return err
}

// Exec runs a command in a running container and returns stdout.
func (c *Client) Exec(ctx context.Context, name string, cmd ...string) ([]byte, error) {
	return c.run(ctx, append([]string{"exec", name}, cmd...)...)
}

// Delete force-removes a container. Missing containers are not an error.
func (c *Client) Delete(ctx context.Context, name string) error {
	_, err := c.run(ctx, "rm", "-f", name)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// Container is the subset of `container ls --format json` kona uses.
type Container struct {
	ID            string `json:"id"`
	Configuration struct {
		Labels map[string]string `json:"labels"`
		Image  struct {
			Reference string `json:"reference"`
		} `json:"image"`
		Resources struct {
			CPUs          int   `json:"cpus"`
			MemoryInBytes int64 `json:"memoryInBytes"`
		} `json:"resources"`
	} `json:"configuration"`
	Status struct {
		State    string `json:"state"`
		Networks []struct {
			Network     string `json:"network"`
			IPv4Address string `json:"ipv4Address"`
			MACAddress  string `json:"macAddress"`
		} `json:"networks"`
	} `json:"status"`
}

// Containers lists all containers, running or not.
func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	out, err := c.run(ctx, "ls", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	var cs []Container
	if err := json.Unmarshal(out, &cs); err != nil {
		return nil, fmt.Errorf("parse container list: %w", err)
	}
	return cs, nil
}

func labelArgs(labels map[string]string) []string {
	var args []string
	for _, k := range sortedKeys(labels) {
		args = append(args, "--label", k+"="+labels[k])
	}
	return args
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
