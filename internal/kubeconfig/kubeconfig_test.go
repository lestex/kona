package kubeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const k3sAdmin = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Q0E=
    server: https://127.0.0.1:6443
  name: default
contexts:
- context:
    cluster: default
    user: default
  name: default
current-context: default
kind: Config
preferences: {}
users:
- name: default
  user:
    client-certificate-data: Q0VSVA==
    client-key-data: S0VZ
`

func TestRewrite(t *testing.T) {
	out, err := Rewrite([]byte(k3sAdmin), "dev", "https://192.168.70.10:6443")
	if err != nil {
		t.Fatal(err)
	}
	var c Config
	if err := yaml.Unmarshal(out, &c); err != nil {
		t.Fatal(err)
	}
	if c.CurrentContext != "kona-dev" || c.Clusters[0].Name != "kona-dev" ||
		c.Clusters[0].Cluster["server"] != "https://192.168.70.10:6443" ||
		c.Clusters[0].Cluster["certificate-authority-data"] != "Q0E=" ||
		c.Users[0].User["client-key-data"] != "S0VZ" ||
		c.Contexts[0].Context["user"] != "kona-dev" {
		t.Fatalf("rewrite result:\n%s", out)
	}
}

func TestMergeAndRemove(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "config")
	existing := `apiVersion: v1
kind: Config
current-context: work
extensions: [{name: keep-me, extension: {a: 1}}]
clusters: [{name: work, cluster: {server: "https://work:6443", extensions: [{name: e, extension: {b: 2}}]}}]
users: [{name: work, user: {token: abc}}]
contexts: [{name: work, context: {cluster: work, user: work}}]
`
	if err := os.WriteFile(dst, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	kona, _ := Rewrite([]byte(k3sAdmin), "dev", "https://192.168.70.10:6443")
	for range 2 { // idempotent
		if err := Merge(dst, kona); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(dst)
	s := string(b)
	if strings.Count(s, "name: kona-dev") != 3 || !strings.Contains(s, "current-context: work") || !strings.Contains(s, "token: abc") ||
		!strings.Contains(s, "keep-me") || !strings.Contains(s, "b: 2") {
		t.Fatalf("merged config:\n%s", s)
	}
	if err := Remove(dst, "dev"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(dst)
	if strings.Contains(string(b), "kona-dev") || !strings.Contains(string(b), "name: work") || !strings.Contains(string(b), "keep-me") {
		t.Fatalf("after remove:\n%s", b)
	}
	if err := Remove(filepath.Join(t.TempDir(), "missing"), "dev"); err != nil {
		t.Fatalf("Remove(missing) = %v", err)
	}
}

func TestWriteIs0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".kube", "kona-dev")
	if err := Write(p, []byte("x")); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}
