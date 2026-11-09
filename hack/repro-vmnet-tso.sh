#!/usr/bin/env bash
# Repro: vmnet hands a sender's TSO super-frames to the receiving VM
# unsegmented. Local delivery accepts them; IP forwarding drops them
# (PKT_TOO_BIG), so large uploads through a forwarding hop stall.
# No Kubernetes: a "router" container forwards to a server in a veth netns.
# usage: hack/repro-vmnet-tso.sh [network] [dns]
set -euo pipefail
net=${1:-default}
dns=${2:-8.8.8.8}
img=alpine:3.22
r=kona-tso-router
size=$((8 << 20))

container rm -f "$r" >/dev/null 2>&1 || true
trap 'container rm -f "$r" >/dev/null 2>&1 || true' EXIT
container run -d --name "$r" --network "$net" --dns "$dns" --cap-add ALL "$img" sleep 900 >/dev/null
rx() { container exec "$r" sh -c "$1"; }
rx 'apk add -q iproute2 >/dev/null
mount -o remount,rw /proc/sys && echo 1 > /proc/sys/net/ipv4/ip_forward
ip netns add srv
ip link add v0 type veth peer name v1
ip link set v1 netns srv
ip addr add 10.99.0.1/24 dev v0; ip link set v0 mtu 1500 up
ip -n srv addr add 10.99.0.2/24 dev v1; ip -n srv link set v1 mtu 1500 up
ip -n srv link set lo up; ip -n srv route add default via 10.99.0.1'
rip=$(rx "ip -4 -o addr show eth0 | awk '{print \$4}' | cut -d/ -f1")

# listen <name> <ns-prefix> <port>: one connection, byte count to /tmp/<name>
listen() { container exec -d "$r" sh -c "$2 nc -l -p $3 | wc -c > /tmp/$1" >/dev/null; sleep 1; }
got() { rx "cat /tmp/$1"; }
# ip_forward counts PKT_TOO_BIG drops as Ip FragFails
fragfails() { rx "awk '/^Ip:/{if(!h){for(i=1;i<=NF;i++)if(\$i==\"FragFails\")c=i;h=1}else print \$c}' /proc/net/snmp"; }

echo "router $rip on $net, server 10.99.0.2 behind it (veth mtu 1500)"
printf '%-6s %-32s %10s %s\n' TSO path received FragFails+
for tso in on off; do
  for path in local forward; do
    if [ "$path" = local ]; then listen t "" 9090; dst="$rip 9090"
    else listen t "ip netns exec srv" 8080; dst="10.99.0.2 8080"; fi
    f0=$(fragfails)
    container run --rm --network "$net" --dns "$dns" --cap-add NET_ADMIN "$img" sh -c "
      apk add -q ethtool >/dev/null; ethtool -K eth0 tso $tso gso $tso
      ip route add 10.99.0.0/24 via $rip
      head -c $size /dev/zero | timeout 15 nc -w 2 $dst" >/dev/null 2>&1 || true
    sleep 1
    rx 'pkill -x nc' || true
    sleep 1
    n=$(got t); f1=$(fragfails)
    label="client -> router (local)"; [ "$path" = forward ] && label="client -> router -> netns (fwd)"
    printf '%-6s %-32s %10s %s\n' "$tso" "$label" "$n/$size" "$((f1 - f0))"
  done
done
