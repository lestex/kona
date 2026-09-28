#!/usr/bin/env bash
# Start an Apple container "node" on the spike network: kona kernel, pinned
# MAC, MTU 1500, a static secondary IP, and (optionally) a published
# WireGuard UDP port.
# usage: spikes/03-net/cpu-up.sh <name> <mac> <static-ip> [wg-port]
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$here/../..
name=${1:?name}
mac=${2:?mac}
static_ip=${3:?static ip}
wg_port=${4:-}
net=${NET:-kona-spike}
subnet=${SUBNET:-192.168.66.0/24}
kernel=$root/kernel/out/vmlinux-6.18.35-kona
dns=$(scutil --dns | awk '/nameserver\[0\]/{print $3; exit}')

[ -f "$kernel" ] || { echo "missing $kernel; run: make -C kernel" >&2; exit 1; }
container network inspect "$net" >/dev/null 2>&1 || container network create "$net" --subnet "$subnet"

args=(-d --progress none --name "$name" --kernel "$kernel" --network "$net,mac=$mac,mtu=1500"
      --dns "$dns" --cap-add ALL)
[ -n "$wg_port" ] && args+=(-p "$wg_port:$wg_port/udp")

container run "${args[@]}" docker.io/library/alpine:3.22 sleep infinity >/dev/null
container exec "$name" sh -c "
apk add -q iperf3 tcpdump iproute2 iputils socat wireguard-tools >/dev/null
ip addr add $static_ip/24 dev eth0
ip -4 -o addr show eth0"
