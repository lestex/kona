// Package state persists cluster inventory under
// ~/Library/Application Support/kona/<cluster>/ (override with KONA_HOME).
//
// cluster.json holds the node inventory (names, roles, IPs, MACs, volumes).
// Secrets live in separate 0600 files next to it so cluster.json can be
// printed and shared safely.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// Cluster lifecycle phases.
const (
	PhaseCreating = "creating"
	PhaseReady    = "ready"
	PhaseFailed   = "failed"
)

// Node kinds.
const (
	KindContainer = "container"
	KindKrunkit   = "krunkit"
)

// Node roles.
const (
	RoleControlPlane = "control-plane"
	RoleWorker       = "worker"
	RoleGPUWorker    = "gpu-worker"
)

// Node is one VM.
type Node struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Kind   string `json:"kind"`
	IP     string `json:"ip"`
	MAC    string `json:"mac"`
	CPUs   int    `json:"cpus"`
	Memory string `json:"memory"`
	Volume string `json:"volume,omitempty"`
}

// Network is the cluster's `container` network.
type Network struct {
	Name    string `json:"name"`
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway"`
	Prefix  int    `json:"prefix"`
}

// Cluster is the persisted inventory.
type Cluster struct {
	Name       string    `json:"name"`
	Distro     string    `json:"distro"`
	CNI        string    `json:"cni"`
	K8sVersion string    `json:"k8sVersion"`
	Image      string    `json:"image"`
	Kernel     string    `json:"kernel"`
	DNS        string    `json:"dns"`
	Phase      string    `json:"phase"`
	Network    Network   `json:"network"`
	Nodes      []Node    `json:"nodes"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ControlPlanes returns the control-plane nodes in order.
func (c *Cluster) ControlPlanes() []Node { return c.byRole(RoleControlPlane) }

func (c *Cluster) byRole(role string) []Node {
	var out []Node
	for _, n := range c.Nodes {
		if n.Role == role {
			out = append(out, n)
		}
	}
	return out
}

// Node returns the named node.
func (c *Cluster) Node(name string) (Node, bool) {
	for _, n := range c.Nodes {
		if n.Name == name {
			return n, true
		}
	}
	return Node{}, false
}

// ErrNotFound is returned for clusters without state.
var ErrNotFound = errors.New("cluster not found")

var nameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,30}[a-z0-9])?$`)

// ValidateName checks a cluster name: DNS-label-like, short enough for node
// names and unix socket paths.
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid cluster name %q: use 1-32 lowercase letters, digits or '-'", name)
	}
	return nil
}

// Store reads and writes cluster state.
type Store struct {
	Root string
}

// Default returns the store at KONA_HOME or ~/Library/Application Support/kona.
func Default() (*Store, error) {
	if h := os.Getenv("KONA_HOME"); h != "" {
		return &Store{Root: h}, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{Root: filepath.Join(dir, "kona")}, nil
}

// Dir returns a cluster's state directory.
func (s *Store) Dir(cluster string) string { return filepath.Join(s.Root, cluster) }

// Save writes cluster.json atomically.
func (s *Store) Save(c *Cluster) error {
	dir := s.Dir(c.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, "cluster.json"), append(b, '\n'), 0o644)
}

// Load reads a cluster's state.
func (s *Store) Load(name string) (*Cluster, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir(name), "cluster.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return nil, err
	}
	var c Cluster
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse state for %s: %w", name, err)
	}
	return &c, nil
}

// List returns every cluster with state, sorted by name. Directories with
// unreadable state are returned as a Cluster with Phase "failed".
func (s *Store) List() ([]*Cluster, error) {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Cluster
	for _, e := range entries {
		if !e.IsDir() || ValidateName(e.Name()) != nil {
			continue
		}
		c, err := s.Load(e.Name())
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			c = &Cluster{Name: e.Name(), Phase: PhaseFailed}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Delete removes a cluster's state directory. Missing state is not an error.
func (s *Store) Delete(name string) error {
	return os.RemoveAll(s.Dir(name))
}

// WriteSecret stores a secret file (0600) in the cluster directory.
func (s *Store) WriteSecret(cluster, name string, data []byte) error {
	dir := s.Dir(cluster)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, name), data, 0o600)
}

// ReadSecret reads a secret file and refuses one that others can read.
func (s *Store) ReadSecret(cluster, name string) ([]byte, error) {
	p := filepath.Join(s.Dir(cluster), name)
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s has permissions %v, want 0600: run chmod 600 %q", p, fi.Mode().Perm(), p)
	}
	return os.ReadFile(p)
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
