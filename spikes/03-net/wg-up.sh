#!/usr/bin/env bash
# Option (c): WireGuard overlay between an Apple container node (cpu1) and a
# krunkit node (gpu0). cpu1's UDP port is published on the host by
# `container run -p 51821:51821/udp`; gpu0 dials the host side of its own
# vmnet network (192.168.65.1) and keeps the path open with keepalives.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
CPU=cpu1

cexec sh -c 'umask 077; [ -f /tmp/wg.key ] || wg genkey > /tmp/wg.key; wg pubkey < /tmp/wg.key > /tmp/wg.pub'
vmssh 'command -v wg >/dev/null || sudo dnf -q -y install wireguard-tools >/dev/null; umask 077; [ -f ~/wg.key ] || wg genkey > ~/wg.key; wg pubkey < ~/wg.key > ~/wg.pub'
cpu_pub=$(cexec cat /tmp/wg.pub)
gpu_pub=$(vmssh cat wg.pub)

cexec sh -c "
ip link del wg0 2>/dev/null || true
ip link add wg0 type wireguard
wg set wg0 private-key /tmp/wg.key listen-port 51821 peer $gpu_pub allowed-ips 10.99.0.10/32
ip addr add 10.99.0.21/24 dev wg0
ip link set wg0 mtu ${WG_MTU:-1420} up"

vmssh "
sudo ip link del wg0 2>/dev/null || true
sudo ip link add wg0 type wireguard
sudo wg set wg0 private-key \$HOME/wg.key peer $cpu_pub endpoint 192.168.65.1:51821 allowed-ips 10.99.0.21/32 persistent-keepalive 25
sudo ip addr add 10.99.0.10/24 dev wg0
sudo ip link set wg0 mtu ${WG_MTU:-1420} up"
echo "wg0 up: cpu1=10.99.0.21 gpu0=10.99.0.10"
