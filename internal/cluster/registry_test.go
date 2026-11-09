package cluster

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/lestex/kona/internal/execx"
)

func TestParseImageRef(t *testing.T) {
	for in, want := range map[string]string{
		"nginx":                       "docker.io/library/nginx:latest",
		"myapp:dev":                   "docker.io/library/myapp:dev",
		"bitnami/redis:7":             "docker.io/bitnami/redis:7",
		"docker.io/library/busybox:1": "docker.io/library/busybox:1",
		"index.docker.io/foo/bar:1":   "docker.io/foo/bar:1",
		"ghcr.io/lestex/app:v1":       "ghcr.io/lestex/app:v1",
		"localhost:5001/x/y:z":        "localhost:5001/x/y:z",
		"registry.example.com:8443/a": "registry.example.com:8443/a:latest",
		"quay.io/org/img:1.2.3-rc.1":  "quay.io/org/img:1.2.3-rc.1",
	} {
		r, err := ParseImageRef(in)
		if err != nil || r.String() != want {
			t.Errorf("ParseImageRef(%q) = %v, %v; want %s", in, r, err, want)
		}
	}
	for _, bad := range []string{"", "nginx@sha256:abc", "ghcr.io/"} {
		if _, err := ParseImageRef(bad); err == nil {
			t.Errorf("ParseImageRef(%q) accepted", bad)
		}
	}
}

func TestMirrorHostsTOML(t *testing.T) {
	got := MirrorHostsTOML("docker.io", "192.168.70.200")
	for _, want := range []string{`server = "https://registry-1.docker.io"`,
		`[host."http://192.168.70.200:5000/v2/docker.io"]`, `capabilities = ["pull", "resolve"]`, "override_path = true"} {
		if !strings.Contains(got, want) {
			t.Errorf("hosts.toml lacks %q:\n%s", want, got)
		}
	}
	if !strings.Contains(MirrorHostsTOML("ghcr.io", "10.0.0.1"), `server = "https://ghcr.io"`) {
		t.Error("ghcr.io upstream")
	}
}

func TestLoadImages(t *testing.T) {
	m, s, log, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inner := m.Container.Runner.(*execx.Fake)
	inner.Calls = nil
	if err := m.LoadImages(context.Background(), "kona", []string{"myapp:dev", "ghcr.io/lestex/tool:v1"}); err != nil {
		t.Fatalf("LoadImages: %v\n%s", err, log)
	}
	c := mustLoad(t, m, "kona")
	if r := c.Registry; r == nil || r.Name != "kona-registry" || r.IP != "192.168.70.200" ||
		strings.Join(r.Hosts, ",") != "docker.io,ghcr.io" {
		t.Fatalf("registry state: %+v", c.Registry)
	}
	if _, ok := s.containers["kona-registry"]; !ok {
		t.Fatal("registry VM not started")
	}
	calls := strings.Join(inner.Calls, "\n")
	for _, want := range []string{
		"image push --scheme http --progress none --platform linux/arm64 192.168.70.200:5000/docker.io/library/myapp:dev",
		"image push --scheme http --progress none --platform linux/arm64 192.168.70.200:5000/ghcr.io/lestex/tool:v1",
		"image rm 192.168.70.200:5000/docker.io/library/myapp:dev",
		"--entrypoint sh",
		"ip -4 addr flush dev eth0 && ip addr add 192.168.70.200/24 dev eth0",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("no %q in:\n%s", want, calls)
		}
	}
	enc := base64.StdEncoding.EncodeToString([]byte(MirrorHostsTOML("ghcr.io", "192.168.70.200")))
	for _, n := range []string{"kona-control-plane-1", "kona-worker-1", "kona-worker-2"} {
		want := "exec " + n + " sh -c mkdir -p \"$(dirname /var/lib/rancher/k3s/agent/etc/containerd/certs.d/ghcr.io/hosts.toml)\" && echo " + enc
		if !strings.Contains(calls, want) {
			t.Errorf("%s did not get the ghcr.io mirror", n)
		}
	}

	// The registry is part of the cluster, not an orphan, and verify checks it.
	if infos, _ := m.List(context.Background()); infos[0].Status != StatusOK || len(infos[0].Issues) != 0 {
		t.Fatalf("list: %+v", infos[0])
	}
	if got := checks(t, m)["registry"]; got.Status != CheckPass {
		t.Fatalf("registry check: %+v", got)
	}

	// A node added later mirrors the same registries.
	inner.Calls = nil
	if err := m.AddNode(context.Background(), AddOptions{Cluster: "kona", CPUs: 2, Memory: "2G", Wait: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(inner.Calls, "\n"), "exec kona-worker-3 sh -c mkdir -p \"$(dirname /var/lib/rancher/k3s/agent/etc/containerd/certs.d/docker.io/hosts.toml)\"") {
		t.Fatal("new node has no mirror config")
	}

	// Stop takes the registry down last; start brings it back first.
	if err := m.Stop(context.Background(), "kona"); err != nil {
		t.Fatal(err)
	}
	if !s.stopped["kona-registry"] {
		t.Fatal("registry still running after stop cluster")
	}
	if err := m.Start(context.Background(), "kona", time.Minute); err != nil {
		t.Fatal(err)
	}
	if s.stopped["kona-registry"] {
		t.Fatal("registry not started")
	}
}

func TestLoadImageNeedsLocalImage(t *testing.T) {
	m, s, _, o := setup(t)
	if err := m.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	s.missingImage = "nope:1"
	err := m.LoadImages(context.Background(), "kona", []string{"nope:1"})
	if err == nil || !strings.Contains(err.Error(), "not in the local") {
		t.Fatalf("got %v", err)
	}
	if mustLoad(t, m, "kona").Registry != nil {
		t.Fatal("registry created for a failed load")
	}
}
