// Package kubeconfig rewrites a distro's admin kubeconfig for use from the
// macOS host and writes it to ~/.kube/kona-<name>. It never touches
// ~/.kube/config unless Merge is called (kona get kubeconfig --merge).
package kubeconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// Config is the subset of a kubeconfig kona edits. Unknown fields inside
// entries are preserved through the raw maps.
type Config struct {
	APIVersion     string         `json:"apiVersion"`
	Kind           string         `json:"kind"`
	Clusters       []Named        `json:"clusters"`
	Contexts       []Named        `json:"contexts"`
	Users          []Named        `json:"users"`
	CurrentContext string         `json:"current-context"`
	Preferences    map[string]any `json:"preferences,omitempty"`
}

// Named is a kubeconfig list entry.
type Named struct {
	Name    string         `json:"name"`
	Cluster map[string]any `json:"cluster,omitempty"`
	Context map[string]any `json:"context,omitempty"`
	User    map[string]any `json:"user,omitempty"`
}

// ContextName is the kube context kona uses for a cluster.
func ContextName(cluster string) string { return "kona-" + cluster }

// Path returns ~/.kube/kona-<cluster>.
func Path(cluster string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kube", "kona-"+cluster), nil
}

// Rewrite renames the single cluster/user/context in raw to kona-<cluster>
// and points it at server.
func Rewrite(raw []byte, cluster, server string) ([]byte, error) {
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse admin kubeconfig: %w", err)
	}
	if len(c.Clusters) != 1 || len(c.Users) != 1 || len(c.Contexts) != 1 {
		return nil, errors.New("admin kubeconfig must have exactly one cluster, user and context")
	}
	name := ContextName(cluster)
	c.Clusters[0].Name = name
	if c.Clusters[0].Cluster == nil {
		c.Clusters[0].Cluster = map[string]any{}
	}
	c.Clusters[0].Cluster["server"] = server
	c.Users[0].Name = name
	c.Contexts[0].Name = name
	c.Contexts[0].Context = map[string]any{"cluster": name, "user": name}
	c.CurrentContext = name
	return yaml.Marshal(c)
}

// Write stores data at path with 0600, creating ~/.kube if needed.
func Write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Merge upserts the kona-<cluster> entries from src into the kubeconfig at
// dst (usually ~/.kube/config), creating it if missing. It works on generic
// maps so fields kona does not model (extensions, exec plugins, ...) are
// preserved, and it does not change dst's current-context.
func Merge(dst string, src []byte) error {
	var in map[string]any
	if err := yaml.Unmarshal(src, &in); err != nil {
		return err
	}
	out, err := readGeneric(dst)
	if err != nil {
		return err
	}
	if out == nil {
		out = map[string]any{"apiVersion": "v1", "kind": "Config"}
	}
	for _, key := range []string{"clusters", "users", "contexts"} {
		out[key] = upsert(list(out[key]), list(in[key]))
	}
	if cc, _ := out["current-context"].(string); cc == "" {
		out["current-context"] = in["current-context"]
	}
	b, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	return Write(dst, b)
}

// Remove deletes the kona-<cluster> entries from the kubeconfig at dst.
// A missing file is not an error.
func Remove(dst, cluster string) error {
	c, err := readGeneric(dst)
	if err != nil || c == nil {
		return err
	}
	name := ContextName(cluster)
	changed := false
	for _, key := range []string{"clusters", "users", "contexts"} {
		before := list(c[key])
		after := drop(before, name)
		if len(after) != len(before) {
			c[key], changed = after, true
		}
	}
	if !changed {
		return nil
	}
	if c["current-context"] == name {
		c["current-context"] = ""
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return Write(dst, out)
}

func readGeneric(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c map[string]any
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func entryName(v any) string {
	m, _ := v.(map[string]any)
	s, _ := m["name"].(string)
	return s
}

func upsert(l, add []any) []any {
	for _, a := range add {
		replaced := false
		for i := range l {
			if entryName(l[i]) == entryName(a) {
				l[i], replaced = a, true
			}
		}
		if !replaced {
			l = append(l, a)
		}
	}
	return l
}

func drop(l []any, name string) []any {
	var out []any
	for _, e := range l {
		if entryName(e) != name {
			out = append(out, e)
		}
	}
	return out
}
