package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadList(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	c := &Cluster{Name: "kona", Distro: "k3s", Phase: PhaseReady, Nodes: []Node{
		{Name: "kona-control-plane-1", Role: RoleControlPlane},
		{Name: "kona-worker-1", Role: RoleWorker},
	}}
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("kona")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ControlPlanes()) != 1 || got.Nodes[1].Name != "kona-worker-1" {
		t.Fatalf("unexpected state: %+v", got)
	}
	all, err := s.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("List = %v, %v", all, err)
	}
	if _, err := s.Load("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load(nope) = %v, want ErrNotFound", err)
	}
}

func TestSecretsAre0600(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	if err := s.WriteSecret("kona", "token", []byte("s3cret")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(s.Dir("kona"), "token"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("token mode = %v, want 0600", fi.Mode().Perm())
	}
	if b, err := s.ReadSecret("kona", "token"); err != nil || string(b) != "s3cret" {
		t.Fatalf("ReadSecret = %q, %v", b, err)
	}
	os.Chmod(filepath.Join(s.Dir("kona"), "token"), 0o644)
	if _, err := s.ReadSecret("kona", "token"); err == nil {
		t.Fatal("ReadSecret accepted a 0644 token")
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"kona", "a", "dev-1"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Kona", "-a", "a-", "a_b", "this-name-is-way-too-long-for-kona-clusters"} {
		if err := ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) = nil", bad)
		}
	}
}
