package version

import "testing"

func TestParse(t *testing.T) {
	got := parse("K3S_VERSION=v1.34.11+k3s1, CONTAINER_VERSION=1.4.1,,bad")
	if len(got) != 2 {
		t.Fatalf("want 2 components, got %d: %v", len(got), got)
	}
	if got[0].Name != "CONTAINER_VERSION" || got[1].Version != "v1.34.11+k3s1" {
		t.Fatalf("unexpected parse result: %v", got)
	}
}

func TestGet(t *testing.T) {
	old := matrix
	t.Cleanup(func() { matrix = old })
	matrix = "KUBERNETES_VERSION=v1.34.12"
	if v := Get("KUBERNETES_VERSION"); v != "v1.34.12" {
		t.Fatalf("Get = %q", v)
	}
	if v := Get("MISSING"); v != "" {
		t.Fatalf("Get(MISSING) = %q", v)
	}
}
