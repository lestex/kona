#!/usr/bin/env bash
# Evidence for options (a)/(b): separate vmnet networks do not forward to
# each other through the host, even though net.inet.ip.forwarding=1 and the
# host has connected routes to both bridges.
set -uo pipefail
source "$(dirname "$0")/lib.sh"
CPU=cpu1
cip=$(cpu_ip)

echo "## host: forwarding + routes"
sysctl net.inet.ip.forwarding
netstat -rn -f inet | grep -E "^192\.168\.6[4-6] "

echo "## (a) vmnet-helper on the container network's subnet (192.168.66.0/24)"
helper=$(brew --prefix vmnet-helper)/libexec/vmnet-helper
sock=/tmp/kona-$(id -u)/iso.sock; rm -f "$sock"
"$helper" --socket "$sock" --interface-id "$(uuidgen)" --operation-mode shared \
  --start-address 192.168.66.1 --end-address 192.168.66.254 --subnet-mask 255.255.255.0 \
  </dev/null >/dev/null 2>/tmp/kona-$(id -u)/iso.log &
hp=$!; sleep 3; kill -9 $hp 2>/dev/null; wait $hp 2>/dev/null
grep -E 'ERROR|started' /tmp/kona-$(id -u)/iso.log; rm -f "$sock"

echo "## (b) container ($cip, bridge102) -> krunkit ($GPU_IP, bridge101) via host"
cexec ping -c2 -W1 "$GPU_IP" | tail -2
echo "## krunkit -> container"
vmssh "ping -c2 -W1 $cip" | tail -2
echo "## where the packets stop: tcpdump on both host bridges while the container pings"
timeout 5 tcpdump -ni bridge102 -c2 icmp 2>/dev/null | sed 's/^/[bridge102 ingress] /' &
timeout 5 tcpdump -ni bridge101 -c2 icmp 2>/dev/null | sed 's/^/[bridge101 egress ] /' &
sleep 1; cexec ping -c2 -W1 "$GPU_IP" >/dev/null; wait
echo "(nothing on bridge101 => not forwarded)"
echo "## two container networks are isolated too: buildkit (default net) -> $cip"
container exec buildkit ping -c2 -W1 "$cip" 2>&1 | tail -2
