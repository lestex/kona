package netplan

import (
	"net/netip"
	"testing"
)

func TestPickSubnetSkipsUsed(t *testing.T) {
	used := []netip.Prefix{
		netip.MustParsePrefix("192.168.64.0/24"),
		netip.MustParsePrefix("192.168.70.0/24"),
		netip.MustParsePrefix("192.168.71.128/25"),
	}
	got, err := PickSubnet(used)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "192.168.72.0/24" {
		t.Fatalf("PickSubnet = %s, want 192.168.72.0/24", got)
	}
}

func TestPickSubnetExhausted(t *testing.T) {
	if _, err := PickSubnet([]netip.Prefix{netip.MustParsePrefix("192.168.0.0/16")}); err == nil {
		t.Fatal("expected error when every candidate is used")
	}
}

func TestLayout(t *testing.T) {
	s := netip.MustParsePrefix("192.168.70.0/24")
	cp, _ := ControlPlaneIP(s, 1)
	w, _ := WorkerIP(s, 2)
	if cp.String() != "192.168.70.10" || w.String() != "192.168.70.21" ||
		Gateway(s).String() != "192.168.70.1" || RegistryIP(s).String() != "192.168.70.200" {
		t.Fatalf("layout: cp=%s w=%s gw=%s reg=%s", cp, w, Gateway(s), RegistryIP(s))
	}
	if _, err := WorkerIP(s, 81); err == nil {
		t.Fatal("worker 81 should be out of range")
	}
}

func TestMACStableAndLocal(t *testing.T) {
	a, b := MAC("kona", "kona-worker-1"), MAC("kona", "kona-worker-1")
	if a != b || a[:9] != "52:54:00:" || a == MAC("kona", "kona-worker-2") {
		t.Fatalf("MAC not stable/unique: %s %s", a, MAC("kona", "kona-worker-2"))
	}
}
