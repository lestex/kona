// Package netplan allocates cluster subnets, static node IPs and MACs.
//
// `container` hands out a different primary address on every run, so kona
// assigns each node a static secondary address from a fixed layout inside
// the cluster's /24 (see docs/networking.md):
//
//	.1        gateway (host)
//	.2-.9     `container`'s own allocations (reserved)
//	.10-.19   control planes
//	.20-.99   workers
//	.200      registry
//
// GPU (krunkit) nodes live on a second /24 served by vmnet-helper:
//
//	.1        gateway (host), also the WireGuard endpoint host for GPU nodes
//	.10-.99   GPU workers
package netplan

import (
	"crypto/sha256"
	"fmt"
	"net"
	"net/netip"
)

// First and last candidate cluster subnets (192.168.70.0/24 .. 192.168.99.0/24).
const (
	firstThirdOctet = 70
	lastThirdOctet  = 99
	Prefix          = 24
)

// Host offsets inside a cluster /24.
const (
	controlPlaneBase = 10
	controlPlaneMax  = 10
	workerBase       = 20
	workerMax        = 80
	RegistryOffset   = 200
	gpuWorkerBase    = 10
	gpuWorkerMax     = 90
)

// FirstWGPort is the lowest host UDP port kona publishes for WireGuard.
const FirstWGPort = 51820

// PickSubnet returns the first candidate /24 that overlaps none of used.
func PickSubnet(used []netip.Prefix) (netip.Prefix, error) {
	for o := firstThirdOctet; o <= lastThirdOctet; o++ {
		p := netip.PrefixFrom(netip.AddrFrom4([4]byte{192, 168, byte(o), 0}), Prefix)
		free := true
		for _, u := range used {
			if u.Overlaps(p) {
				free = false
				break
			}
		}
		if free {
			return p, nil
		}
	}
	return netip.Prefix{}, fmt.Errorf("no free /24 in 192.168.%d.0-192.168.%d.0: delete unused `container` networks", firstThirdOctet, lastThirdOctet)
}

// HostPrefixes returns the prefixes of the host's own IPv4 interfaces.
func HostPrefixes() []netip.Prefix {
	var out []netip.Prefix
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if p, err := netip.ParsePrefix(ipn.String()); err == nil && p.Addr().Is4() {
				out = append(out, p.Masked())
			}
		}
	}
	return out
}

// Gateway returns the .1 address of subnet.
func Gateway(subnet netip.Prefix) netip.Addr { return offset(subnet, 1) }

// ControlPlaneIP returns the address of control plane i (1-based).
func ControlPlaneIP(subnet netip.Prefix, i int) (netip.Addr, error) {
	if i < 1 || i > controlPlaneMax {
		return netip.Addr{}, fmt.Errorf("control plane index %d out of range 1-%d", i, controlPlaneMax)
	}
	return offset(subnet, controlPlaneBase+i-1), nil
}

// WorkerIP returns the address of worker i (1-based).
func WorkerIP(subnet netip.Prefix, i int) (netip.Addr, error) {
	if i < 1 || i > workerMax {
		return netip.Addr{}, fmt.Errorf("worker index %d out of range 1-%d", i, workerMax)
	}
	return offset(subnet, workerBase+i-1), nil
}

// GPUWorkerIP returns the address of GPU worker i (1-based) in the GPU subnet.
func GPUWorkerIP(subnet netip.Prefix, i int) (netip.Addr, error) {
	if i < 1 || i > gpuWorkerMax {
		return netip.Addr{}, fmt.Errorf("GPU worker index %d out of range 1-%d", i, gpuWorkerMax)
	}
	return offset(subnet, gpuWorkerBase+i-1), nil
}

// PickPorts returns n host UDP ports from FirstWGPort upwards that are not
// in used and that free() accepts (free may be nil).
func PickPorts(n int, used map[int]bool, free func(int) bool) ([]int, error) {
	var out []int
	for p := FirstWGPort; p < FirstWGPort+2000 && len(out) < n; p++ {
		if used[p] || (free != nil && !free(p)) {
			continue
		}
		out = append(out, p)
	}
	if len(out) < n {
		return nil, fmt.Errorf("no free UDP ports for WireGuard in %d-%d", FirstWGPort, FirstWGPort+1999)
	}
	return out, nil
}

// UDPPortFree reports whether a UDP port can be bound on the host.
func UDPPortFree(p int) bool {
	c, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", p))
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// RegistryIP returns the registry address.
func RegistryIP(subnet netip.Prefix) netip.Addr { return offset(subnet, RegistryOffset) }

func offset(subnet netip.Prefix, n int) netip.Addr {
	b := subnet.Masked().Addr().As4()
	b[3] = byte(n)
	return netip.AddrFrom4(b)
}

// MAC returns a stable, locally administered MAC for a node. The QEMU/KVM
// OUI 52:54:00 is used by convention for virtual NICs.
func MAC(cluster, node string) string {
	h := sha256.Sum256([]byte(cluster + "/" + node))
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", h[0], h[1], h[2])
}
