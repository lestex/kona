package cluster

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// redacted replaces secret values in a support bundle.
const redacted = "<redacted>"

// SupportBundle writes a gzipped tar of everything useful for debugging a
// cluster: state, kona's own checks, `container`'s view, per-node logs and
// network config, and the cluster's nodes, pods and events. extra holds
// files the caller adds (version, doctor). Every collection step is
// independent: a failure is written into its file instead of aborting.
// The cluster's secrets (join tokens, kubeconfig) are never included, and
// their values are redacted wherever they appear in collected output.
func (m *Manager) SupportBundle(ctx context.Context, name string, w io.Writer, extra map[string][]byte) error {
	c, err := m.Store.Load(name)
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	for k, v := range extra {
		files[k] = v
	}
	add := func(file string, r result) {
		out := r.out
		if r.err != nil {
			out = append(out, []byte("\nerror: "+r.err.Error()+"\n")...)
		}
		files[file] = out
	}
	asJSON := func(v any, err error) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		return json.MarshalIndent(v, "", "  ")
	}
	host := func(args ...string) ([]byte, error) { return m.Host.Run(ctx, args[0], args[1:]...) }

	add("cluster.json", res(os.ReadFile(filepath.Join(m.Store.Dir(name), "cluster.json"))))
	add("verify.json", res(asJSON(m.Verify(ctx, name))))
	add("nodes.json", res(asJSON(m.Nodes(ctx, name))))

	for file, args := range map[string][]string{
		"container/version.txt":       {"container", "--version"},
		"container/system-status.txt": {"container", "system", "status"},
		"container/ls.txt":            {"container", "ls", "--all"},
		"container/networks.txt":      {"container", "network", "ls"},
		"container/volumes.txt":       {"container", "volume", "ls"},
		"host/sw_vers.txt":            {"sw_vers"},
		"host/dns.txt":                {"scutil", "--dns"},
	} {
		add(file, res(host(args...)))
	}

	d, derr := DistroFor(c.Distro)
	for _, n := range c.Nodes {
		dir := "nodes/" + n.Name + "/"
		add(dir+"container-logs.txt", res(host("container", "logs", n.Name)))
		for file, cmd := range map[string]string{
			"ip.txt":    "ip addr; echo; ip route; echo; cat /etc/resolv.conf",
			"dmesg.txt": "dmesg | tail -n 300",
			"ps.txt":    "ps -eo pid,ppid,stat,etime,args",
			"df.txt":    "df -h",
			// systemd nodes (kubeadm) log to the journal; k3s nodes log to
			// the container's stdout (container-logs.txt).
			"journal.txt": "command -v journalctl >/dev/null || { echo 'no journal on this node'; exit 0; }; " +
				"journalctl --no-pager -n 2000 -u kubelet -u containerd",
		} {
			add(dir+file, res(m.Container.Exec(ctx, n.Name, "sh", "-c", cmd)))
		}
	}

	if cps := c.ControlPlanes(); len(cps) > 0 && derr == nil {
		for file, args := range map[string][]string{
			"kubernetes/nodes.txt":          {"get", "nodes", "-o", "wide"},
			"kubernetes/pods.txt":           {"get", "pods", "-A", "-o", "wide"},
			"kubernetes/events.txt":         {"get", "events", "-A", "--sort-by=.lastTimestamp"},
			"kubernetes/describe-nodes.txt": {"describe", "nodes"},
			"kubernetes/daemonsets.txt":     {"get", "daemonsets", "-A", "-o", "wide"},
		} {
			add(file, res(m.Container.Exec(ctx, cps[0].Name, d.Kubectl(args...)...)))
		}
	}

	return writeBundle(w, "kona-"+name+"-support", files, m.secretValues(name))
}

// result is one collected command's output and error.
type result struct {
	out []byte
	err error
}

func res(out []byte, err error) result { return result{out, err} }

// secretValues returns the cluster's secret values to redact, longest first
// so a value that contains another is replaced whole. It only reads: a
// bundle never creates or changes secrets.
func (m *Manager) secretValues(cluster string) []string {
	vals := map[string]string{}
	if b, err := m.Store.ReadSecret(cluster, secretsFile); err == nil {
		_ = json.Unmarshal(b, &vals)
	}
	if b, err := m.Store.ReadSecret(cluster, legacyTokenFile); err == nil {
		vals[legacyTokenFile] = strings.TrimSpace(string(b))
	}
	var out []string
	for _, v := range vals {
		// Short values would redact ordinary words.
		if len(v) >= 8 {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

func writeBundle(w io.Writer, root string, files map[string][]byte, secrets []string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		body := string(files[n])
		for _, s := range secrets {
			body = strings.ReplaceAll(body, s, redacted)
		}
		if err := tw.WriteHeader(&tar.Header{Name: root + "/" + n, Mode: 0o644, Size: int64(len(body))}); err != nil {
			return err
		}
		if _, err := io.WriteString(tw, body); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}
	return nil
}
