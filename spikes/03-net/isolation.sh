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

echo "## (b) container ($cip) -> krunkit ($GPU_IP) via host"
cexec ping -c2 -W1 "$GPU_IP" | tail -2
echo "## krunkit -> container"
vmssh "ping -c2 -W1 $cip" | tail -2
cbr=$(bridge_for "$(echo "$cip" | cut -d. -f1-3).1")
gbr=$(bridge_for "$(echo "$GPU_IP" | cut -d. -f1-3).1")
echo "## where the packets stop: tcpdump on both host bridges while the container pings"
timeout 5 tcpdump -ni "$cbr" -c2 icmp 2>/dev/null | sed "s/^/[$cbr ingress] /" &
timeout 5 tcpdump -ni "$gbr" -c2 icmp 2>/dev/null | sed "s/^/[$gbr egress ] /" &
sleep 1; cexec ping -c2 -W1 "$GPU_IP" >/dev/null; wait
echo "(nothing on $gbr => not forwarded)"
echo "## two container networks are isolated too: a VM on another container network -> $cip"
if container exec cpu0 true 2>/dev/null && [ "$(CPU=cpu0 cpu_ip | cut -d. -f1-3)" != "$(echo "$cip" | cut -d. -f1-3)" ]; then
  CPU=cpu0 cexec ping -c2 -W1 "$cip" 2>&1 | tail -2
elif container exec buildkit true 2>/dev/null; then
  container exec buildkit ping -c2 -W1 "$cip" 2>&1 | tail -2
else
  echo "(skipped: start a container on another network, e.g. NET=default spikes/03-net/cpu-up.sh cpu0 ...)"
fi

exit 0  # failures above are the expected demonstration
