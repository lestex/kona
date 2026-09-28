package wg

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestKnownKeyPair(t *testing.T) {
	// RFC 7748 section 6.1 test vector (Alice).
	priv := "dwdtCnMYpX08FsFyUbJmRd9ML4frwJkqsXf7pR25LCo="
	want := "hSDwCYkwp1R0i33ctD73Wg2/Og0mOBr066SpjqqbTmo="
	got, err := PublicKey(priv)
	if err != nil || got != want {
		t.Fatalf("PublicKey = %s, %v; want %s", got, err, want)
	}
}

func TestGenerateKeyClamped(t *testing.T) {
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := base64.StdEncoding.DecodeString(k)
	if len(b) != 32 || b[0]&7 != 0 || b[31]&128 != 0 || b[31]&64 == 0 {
		t.Fatalf("key not clamped: %x", b)
	}
	if _, err := PublicKey(k); err != nil {
		t.Fatal(err)
	}
}

func TestRender(t *testing.T) {
	conf, routes := Config{
		PrivateKey: "PRIV",
		Peers: []Peer{
			{Name: "kona-worker-1", PublicKey: "W1", IP: "192.168.70.20", Endpoint: "192.168.71.1:51821"},
			{Name: "kona-control-plane-1", PublicKey: "CP", IP: "192.168.70.10", Endpoint: "192.168.71.1:51820"},
		},
		Routes: []string{"192.168.70.0/24"},
	}.Render()
	want := `[Interface]
PrivateKey = PRIV
ListenPort = 51820

# kona-control-plane-1
[Peer]
PublicKey = CP
AllowedIPs = 192.168.70.10/32
Endpoint = 192.168.71.1:51820
PersistentKeepalive = 25

# kona-worker-1
[Peer]
PublicKey = W1
AllowedIPs = 192.168.70.20/32
Endpoint = 192.168.71.1:51821
PersistentKeepalive = 25
`
	if conf != want {
		t.Fatalf("conf:\n%s\nwant:\n%s", conf, want)
	}
	if routes != "192.168.70.0/24\n" {
		t.Fatalf("routes %q", routes)
	}
	cpu, _ := Config{PrivateKey: "P", Peers: []Peer{{Name: "g", PublicKey: "G", IP: "192.168.71.10"}}}.Render()
	if strings.Contains(cpu, "Endpoint") || strings.Contains(cpu, "Keepalive") {
		t.Fatalf("CPU side must learn endpoints: %s", cpu)
	}
}
