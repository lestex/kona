package deploy

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestManifestIsValidYAMLAndConsistent(t *testing.T) {
	docs := bytes.Split(VenusDRA, []byte("\n---\n"))
	kinds := map[string]bool{}
	for _, d := range docs {
		var m map[string]any
		if err := yaml.Unmarshal(d, &m); err != nil {
			t.Fatalf("invalid YAML: %v\n%s", err, d)
		}
		kinds[m["kind"].(string)] = true
	}
	for _, k := range []string{"Namespace", "ServiceAccount", "ClusterRole", "ClusterRoleBinding", "DeviceClass", "DaemonSet"} {
		if !kinds[k] {
			t.Errorf("manifest has no %s", k)
		}
	}
	if !bytes.Contains(VenusDRA, []byte("image: "+VenusDRAImage)) {
		t.Errorf("DaemonSet image differs from VenusDRAImage %s", VenusDRAImage)
	}
	// The driver name and version must match the driver module and versions.env.
	consts, err := os.ReadFile("../dra/kona-venus-dra/pkg/driver/consts.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(consts, []byte(`DriverName = "venus.kona.example.invalid"`)) ||
		!bytes.Contains(VenusDRA, []byte(`device.driver == "venus.kona.example.invalid"`)) {
		t.Error("driver name differs between consts.go and the DeviceClass")
	}
	env, _ := os.ReadFile("../versions.env")
	if !strings.Contains(string(env), "KONA_VENUS_DRA_VERSION="+strings.Split(VenusDRAImage, ":")[1]+"\n") {
		t.Error("image tag differs from KONA_VENUS_DRA_VERSION in versions.env")
	}
}
