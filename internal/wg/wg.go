// Package wg renders the WireGuard link between a cluster's `container`
// network (CPU nodes) and its krunkit GPU network.
//
// Separate vmnet networks cannot reach each other, so GPU nodes dial each
// CPU node's WireGuard port, published on the host by `container run -p`, at
// their own vmnet gateway. CPU nodes learn GPU endpoints from the handshake.
// AllowedIPs are node /32s only: both CNIs tunnel pod traffic (VXLAN) with
// node IPs on the outside. See docs/networking.md.
package wg

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// ListenPort is the WireGuard port inside every node.
const ListenPort = 51820

// Keepalive keeps the host's UDP proxy flow alive (seconds).
const Keepalive = 25

// GenerateKey returns a base64 WireGuard private key.
func GenerateKey() (string, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return "", err
	}
	// Clamp as in wg genkey.
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
	return base64.StdEncoding.EncodeToString(k[:]), nil
}

// PublicKey derives the base64 public key from a base64 private key.
func PublicKey(private string) (string, error) {
	priv, err := base64.StdEncoding.DecodeString(private)
	if err != nil || len(priv) != 32 {
		return "", fmt.Errorf("invalid WireGuard private key")
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

// Peer is one node as seen by the other side.
type Peer struct {
	Name      string
	PublicKey string
	IP        string // node IP, AllowedIPs = IP/32
	// Endpoint is host:port to dial (GPU side only; empty on CPU side).
	Endpoint string
}

// Config is one node's wg0 setup.
type Config struct {
	PrivateKey string
	Peers      []Peer
	// Routes are prefixes (the other network's subnet) routed via wg0.
	Routes []string
}

// Render returns the `wg setconf` file (/etc/kona/wg0.conf) and the routes
// file (/etc/kona/wg-routes) consumed by images/node/kona-wg.
func (c Config) Render() (conf, routes string) {
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nListenPort = %d\n", c.PrivateKey, ListenPort)
	peers := append([]Peer(nil), c.Peers...)
	sort.Slice(peers, func(i, j int) bool { return peers[i].Name < peers[j].Name })
	for _, p := range peers {
		fmt.Fprintf(&b, "\n# %s\n[Peer]\nPublicKey = %s\nAllowedIPs = %s/32\n", p.Name, p.PublicKey, p.IP)
		if p.Endpoint != "" {
			fmt.Fprintf(&b, "Endpoint = %s\nPersistentKeepalive = %d\n", p.Endpoint, Keepalive)
		}
	}
	return b.String(), strings.Join(c.Routes, "\n") + "\n"
}
