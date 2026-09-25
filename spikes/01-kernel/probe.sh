#!/usr/bin/env bash
# Phase 0 kernel spike: boot one Apple container VM with a given kernel and
# probe the features kona needs.
# usage: spikes/01-kernel/probe.sh [kernel-path]   (default: system default kernel)
set -uo pipefail

kernel=${1:-}
dns=$(scutil --dns | awk '/nameserver\[0\]/{print $3; exit}')
args=(--rm --dns "$dns" --cap-add ALL)
[ -n "$kernel" ] && args+=(--kernel "$kernel")

container run "${args[@]}" docker.io/library/alpine:3.22 sh -c '
echo "## uname -r"; uname -r
echo "## lsmod"; lsmod 2>&1; echo "(no /proc/modules => CONFIG_MODULES=n, everything built in)"
echo "## cgroup2"; mount | grep cgroup2; cat /sys/fs/cgroup/cgroup.controllers
apk add -q bpftool iproute2 iproute2-tc nftables >/dev/null
echo "## bpftool feature probe kernel (filtered)"
bpftool feature probe kernel 2>/dev/null | grep -E "CONFIG_(BPF_SYSCALL|BPF_JIT|CGROUP_BPF|NET_CLS_BPF|NET_SCH_INGRESS|NET_CLS_ACT|NET_ACT_BPF|BPF_EVENTS|DEBUG_INFO_BTF)[ =]|JIT compiler is|program_type (sched_cls|sched_act|cgroup_skb|cgroup_sock_addr|xdp) is|map_type (lpm_trie|hash_of_maps|lru_hash|ringbuf) is"
echo "## link types"
for t in vxlan geneve wireguard dummy netkit; do
  case $t in vxlan) extra="id 1 dstport 4789";; geneve) extra="id 1 remote 10.0.0.1";; *) extra="";; esac
  if ip link add kt-$t type $t $extra >/dev/null 2>&1; then echo "$t ok"; ip link del kt-$t; else echo "$t FAIL"; fi
done
echo "## tc clsact"; tc qdisc add dev eth0 clsact 2>&1 && tc qdisc show dev eth0 | grep clsact && tc qdisc del dev eth0 clsact
echo "## br_netfilter"; ls /proc/sys/net/bridge/ 2>&1 | head -3
echo "## ip_vs"; head -1 /proc/net/ip_vs 2>&1
echo "## nf_tables"; nft add table inet kt && nft list tables && nft delete table inet kt
echo "## eth0 mtu"; cat /sys/class/net/eth0/mtu
' 2>&1 | grep -v -E '^\[[0-9]/[0-9]\]'
