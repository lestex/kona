package kubeadm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lestex/kona/internal/state"
	"sigs.k8s.io/yaml"
)

func testCluster() *state.Cluster {
	return &state.Cluster{Name: "kona", K8sVersion: "v1.34.12", Nodes: []state.Node{
		{Name: "kona-control-plane-1", Role: state.RoleControlPlane, IP: "192.168.70.10"},
		{Name: "kona-control-plane-2", Role: state.RoleControlPlane, IP: "192.168.70.11"},
		{Name: "kona-worker-1", Role: state.RoleWorker, IP: "192.168.70.20"},
	}}
}

func TestSecretsFormat(t *testing.T) {
	s, err := Kubeadm{}.NewSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-z0-9]{6}\.[a-z0-9]{16}$`).MatchString(s[SecretBootstrapToken]) {
		t.Fatalf("bootstrap token %q does not match kubeadm format", s[SecretBootstrapToken])
	}
	if len(s[SecretCertificateKey]) != 64 {
		t.Fatalf("certificate key %q", s[SecretCertificateKey])
	}
}

func TestConfigsAreValidYAML(t *testing.T) {
	c := testCluster()
	s := map[string]string{SecretBootstrapToken: "abcdef.0123456789abcdef", SecretCertificateKey: "k"}
	init, err := initConfig(c, c.Nodes[0], s)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range strings.Split(string(init), "\n---\n") {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatalf("init config doc invalid: %v\n%s", err, doc)
		}
	}
	for _, want := range []string{"advertiseAddress: 192.168.70.10", "controlPlaneEndpoint: 192.168.70.10:6443",
		"dataDir: /var/lib/etcd/data", "- 192.168.70.11", `ttl: "0s"`, "kubernetesVersion: v1.34.12"} {
		if !strings.Contains(string(init), want) {
			t.Errorf("init config missing %q:\n%s", want, init)
		}
	}
	cpJoin, _ := joinConfig(c, c.Nodes[1], s, "sha256:aa")
	wJoin, _ := joinConfig(c, c.Nodes[2], s, "sha256:aa")
	for _, j := range [][]byte{cpJoin, wJoin} {
		var m map[string]any
		if err := yaml.Unmarshal(j, &m); err != nil {
			t.Fatalf("join config invalid: %v\n%s", err, j)
		}
	}
	if !strings.Contains(string(cpJoin), "certificateKey") || strings.Contains(string(wJoin), "controlPlane:") {
		t.Fatalf("control-plane section wrong:\n%s\n---\n%s", cpJoin, wJoin)
	}
}

func TestCAHash(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kubernetes"},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(der)
	want := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	got, err := CAHash(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	if err != nil || got != "sha256:"+hex.EncodeToString(want[:]) {
		t.Fatalf("CAHash = %s, %v", got, err)
	}
}

// fakeExec answers `test -f` from a set of existing files and records the rest.
type fakeExec struct {
	files map[string]bool
	calls []string
}

func (f *fakeExec) Exec(_ context.Context, node string, cmd ...string) ([]byte, error) {
	line := node + ": " + strings.Join(cmd, " ")
	f.calls = append(f.calls, line)
	if cmd[0] == "test" {
		if f.files[node+":"+cmd[2]] {
			return nil, nil
		}
		return nil, errors.New("exit 1")
	}
	if cmd[0] == "sh" { // writeFile: remember the decoded file
		parts := strings.Fields(cmd[2])
		for i, p := range parts {
			if p == "echo" {
				if _, err := base64.StdEncoding.DecodeString(parts[i+1]); err != nil {
					return nil, err
				}
			}
		}
	}
	return nil, nil
}

func TestBootstrapInitThenIdempotent(t *testing.T) {
	c := testCluster()
	s, _ := Kubeadm{}.NewSecrets()
	x := &fakeExec{files: map[string]bool{}}
	k := Kubeadm{Sleep: func(time.Duration) {}}
	if err := k.Bootstrap(context.Background(), x, c, c.Nodes[0], s); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(x.calls, "\n")
	if !strings.Contains(joined, "kubeadm init --config "+ConfigPath+" --upload-certs") ||
		!strings.Contains(joined, "kubectl --kubeconfig "+AdminKubeconfigPath+" apply -f /etc/kubernetes/kona-flannel.yml") {
		t.Fatalf("calls:\n%s", joined)
	}
	x.files["kona-control-plane-1:"+AdminKubeconfigPath] = true
	x.calls = nil
	if err := k.Bootstrap(context.Background(), x, c, c.Nodes[0], s); err != nil {
		t.Fatal(err)
	}
	for _, call := range x.calls {
		if strings.Contains(call, "kubeadm") {
			t.Fatalf("re-ran kubeadm on an initialized node: %s", call)
		}
	}
}
