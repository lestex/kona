// Package kubeadm bootstraps clusters with upstream kubeadm on systemd node
// images (opt-in distro: closest to production layout; see docs/distros.md).
package kubeadm

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/lestex/kona/internal/distro"
	"github.com/lestex/kona/internal/state"
	"github.com/lestex/kona/internal/version"
)

// Paths inside the node.
const (
	AdminKubeconfigPath = "/etc/kubernetes/admin.conf"
	ConfigPath          = "/etc/kubernetes/kona-kubeadm.yaml"
	// DataDir is the block volume's mount point. etcd uses a subdirectory:
	// a fresh ext4 volume contains lost+found, and kubeadm's preflight
	// requires an empty etcd data dir.
	DataDir         = "/var/lib/etcd"
	EtcdDataDir     = DataDir + "/data"
	caPath          = "/etc/kubernetes/pki/ca.crt"
	kubeletConfPath = "/etc/kubernetes/kubelet.conf"
)

// Secret keys.
const (
	SecretBootstrapToken = "bootstrapToken"
	SecretCertificateKey = "certificateKey"
)

// Network ranges (flannel default pod CIDR).
const (
	PodSubnet     = "10.244.0.0/16"
	ServiceSubnet = "10.96.0.0/12"
)

//go:embed manifests/kube-flannel.yml
var flannelManifest []byte

// Kubeadm implements distro.Distro.
type Kubeadm struct {
	// Sleep is used while waiting for systemd (overridable in tests).
	Sleep func(time.Duration)
}

var _ distro.Distro = Kubeadm{}

// Name implements distro.Distro.
func (Kubeadm) Name() string { return "kubeadm" }

// DefaultImage implements distro.Distro.
func (Kubeadm) DefaultImage() string {
	return "ghcr.io/lestex/kona-node:" + version.Get("KUBERNETES_VERSION") + "-kubeadm"
}

// NewSecrets implements distro.Distro: a non-expiring bootstrap token (kept
// 0600 by kona for `kona add node`) and the key that encrypts control-plane
// certificates uploaded for HA joins.
func (Kubeadm) NewSecrets() (distro.Secrets, error) {
	id, err := randomLower(6)
	if err != nil {
		return nil, err
	}
	secret, err := randomLower(16)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return distro.Secrets{
		SecretBootstrapToken: id + "." + secret,
		SecretCertificateKey: hex.EncodeToString(key),
	}, nil
}

func randomLower(n int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

// NodeSpec implements distro.Distro: the image runs systemd as PID 1.
func (Kubeadm) NodeSpec(*state.Cluster, state.Node, distro.Secrets) (distro.NodeSpec, error) {
	return distro.NodeSpec{Tmpfs: []string{"/run", "/tmp"}, DataDir: DataDir}, nil
}

// Bootstrap implements distro.Distro.
func (k Kubeadm) Bootstrap(ctx context.Context, x distro.Execer, c *state.Cluster, n state.Node, s distro.Secrets) error {
	if err := k.waitSystemd(ctx, x, n.Name); err != nil {
		return err
	}
	cps := c.ControlPlanes()
	first := n.Name == cps[0].Name
	done := kubeletConfPath
	if first {
		done = AdminKubeconfigPath
	}
	if _, err := x.Exec(ctx, n.Name, "test", "-f", done); err == nil {
		return nil // already initialized/joined (resume)
	}
	var cfg []byte
	var err error
	var cmd []string
	if first {
		cfg, err = initConfig(c, n, s)
		cmd = []string{"kubeadm", "init", "--config", ConfigPath, "--upload-certs"}
		if c.CNI == "cilium" {
			// Cilium runs with kubeProxyReplacement=true.
			cmd = append(cmd, "--skip-phases=addon/kube-proxy")
		}
	} else {
		var hash string
		if hash, err = caHash(ctx, x, cps[0].Name); err != nil {
			return err
		}
		cfg, err = joinConfig(c, n, s, hash)
		cmd = []string{"kubeadm", "join", "--config", ConfigPath}
	}
	if err != nil {
		return err
	}
	if err := writeFile(ctx, x, n.Name, ConfigPath, cfg); err != nil {
		return err
	}
	if out, err := x.Exec(ctx, n.Name, cmd...); err != nil {
		return fmt.Errorf("%s on %s: %w\n%s", strings.Join(cmd[:2], " "), n.Name, err, out)
	}
	if first && c.CNI != "cilium" {
		if err := writeFile(ctx, x, n.Name, "/etc/kubernetes/kona-flannel.yml", flannelManifest); err != nil {
			return err
		}
		if _, err := x.Exec(ctx, n.Name, Kubeadm{}.Kubectl("apply", "-f", "/etc/kubernetes/kona-flannel.yml")...); err != nil {
			return fmt.Errorf("apply flannel: %w", err)
		}
	}
	return nil
}

func (k Kubeadm) waitSystemd(ctx context.Context, x distro.Execer, node string) error {
	sleep := k.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	var last error
	for range 60 {
		if _, last = x.Exec(ctx, node, "systemctl", "is-active", "--quiet", "containerd"); last == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		sleep(time.Second)
	}
	return fmt.Errorf("containerd did not start on %s: %v (see `kona ssh %s -- journalctl -u containerd`)", node, last, node)
}

// writeFile writes data to path in the node via base64 so no shell quoting
// of the content is needed.
func writeFile(ctx context.Context, x distro.Execer, node, path string, data []byte) error {
	enc := base64Std(data)
	script := fmt.Sprintf("mkdir -p \"$(dirname %s)\" && echo %s | base64 -d > %s && chmod 0600 %s", path, enc, path, path)
	_, err := x.Exec(ctx, node, "sh", "-c", script)
	return err
}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// caHash returns kubeadm's discovery hash: sha256 of the CA certificate's
// SubjectPublicKeyInfo.
func caHash(ctx context.Context, x distro.Execer, cp string) (string, error) {
	out, err := x.Exec(ctx, cp, "cat", caPath)
	if err != nil {
		return "", fmt.Errorf("read cluster CA from %s: %w", cp, err)
	}
	return CAHash(out)
}

// CAHash computes the discovery hash of a PEM CA certificate.
func CAHash(pemData []byte) (string, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return "", errors.New("cluster CA is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

var initTmpl = template.Must(template.New("init").Parse(`apiVersion: kubeadm.k8s.io/v1beta4
kind: InitConfiguration
bootstrapTokens:
- token: "{{.Token}}"
  ttl: "0s"
  description: "kona: joins nodes (kona add node); stored 0600 in the kona state dir"
certificateKey: "{{.CertKey}}"
localAPIEndpoint:
  advertiseAddress: {{.IP}}
  bindPort: 6443
nodeRegistration:
  name: {{.Name}}
  criSocket: unix:///run/containerd/containerd.sock
  kubeletExtraArgs:
  - {name: node-ip, value: {{.IP}}}
  - {name: node-labels, value: "kona.dev/cluster={{.Cluster}},kona.dev/role=control-plane"}
---
apiVersion: kubeadm.k8s.io/v1beta4
kind: ClusterConfiguration
clusterName: {{.Cluster}}
kubernetesVersion: {{.Version}}
controlPlaneEndpoint: {{.IP}}:6443
networking:
  podSubnet: {{.PodSubnet}}
  serviceSubnet: {{.ServiceSubnet}}
apiServer:
  certSANs:{{range .SANs}}
  - {{.}}{{end}}
etcd:
  local:
    dataDir: {{.DataDir}}
---
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
cgroupDriver: systemd
failSwapOn: false
`))

var joinTmpl = template.Must(template.New("join").Parse(`apiVersion: kubeadm.k8s.io/v1beta4
kind: JoinConfiguration
discovery:
  bootstrapToken:
    apiServerEndpoint: {{.Endpoint}}
    token: "{{.Token}}"
    caCertHashes: ["{{.CAHash}}"]
nodeRegistration:
  name: {{.Name}}
  criSocket: unix:///run/containerd/containerd.sock
  kubeletExtraArgs:
  - {name: node-ip, value: {{.IP}}}
  - {name: node-labels, value: "kona.dev/cluster={{.Cluster}},kona.dev/role={{.Role}}"}
{{- if .ControlPlane}}
controlPlane:
  certificateKey: "{{.CertKey}}"
  localAPIEndpoint:
    advertiseAddress: {{.IP}}
    bindPort: 6443
{{- end}}
`))

func initConfig(c *state.Cluster, n state.Node, s distro.Secrets) ([]byte, error) {
	sans := []string{}
	for _, cp := range c.ControlPlanes() {
		sans = append(sans, cp.IP, cp.Name)
	}
	v := c.K8sVersion
	if v == "" {
		v = version.Get("KUBERNETES_VERSION")
	}
	var b bytes.Buffer
	err := initTmpl.Execute(&b, map[string]any{
		"Token": s[SecretBootstrapToken], "CertKey": s[SecretCertificateKey],
		"IP": n.IP, "Name": n.Name, "Cluster": c.Name, "Version": v,
		"PodSubnet": PodSubnet, "ServiceSubnet": ServiceSubnet, "SANs": sans, "DataDir": EtcdDataDir,
	})
	return b.Bytes(), err
}

func joinConfig(c *state.Cluster, n state.Node, s distro.Secrets, caHash string) ([]byte, error) {
	var b bytes.Buffer
	err := joinTmpl.Execute(&b, map[string]any{
		"Endpoint": c.ControlPlanes()[0].IP + ":6443", "Token": s[SecretBootstrapToken],
		"CAHash": caHash, "Name": n.Name, "IP": n.IP, "Cluster": c.Name, "Role": n.Role,
		"ControlPlane": n.Role == state.RoleControlPlane, "CertKey": s[SecretCertificateKey],
	})
	return b.Bytes(), err
}

// AdminKubeconfig implements distro.Distro.
func (Kubeadm) AdminKubeconfig(ctx context.Context, x distro.Execer, c *state.Cluster) ([]byte, error) {
	return x.Exec(ctx, c.ControlPlanes()[0].Name, "cat", AdminKubeconfigPath)
}

// Kubectl implements distro.Distro.
func (Kubeadm) Kubectl(args ...string) []string {
	return append([]string{"kubectl", "--kubeconfig", AdminKubeconfigPath}, args...)
}
