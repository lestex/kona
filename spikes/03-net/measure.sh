#!/usr/bin/env bash
# Measure the cpu1 <-> gpu0 path: overlay source IPs (no NAT inside the
# tunnel), path MTU, and throughput both ways.
set -uo pipefail
source "$(dirname "$0")/lib.sh"
CPU=cpu1

echo "## underlay MTU"
echo "cpu1 eth0: $(cexec cat /sys/class/net/eth0/mtu)  gpu0 eth0: $(vmssh cat /sys/class/net/eth0/mtu)  wg0: $(cexec cat /sys/class/net/wg0/mtu)"

echo "## source IP as seen by the receiver (tcpdump on wg0)"
vmssh 'sudo timeout 6 tcpdump -ni wg0 -c1 icmp 2>/dev/null' &
sleep 2; cexec ping -c1 -W1 10.99.0.10 >/dev/null; wait
cexec sh -c 'timeout 6 tcpdump -ni wg0 -c1 icmp 2>/dev/null' &
sleep 2; vmssh 'ping -c1 -W1 10.99.0.21 >/dev/null'; wait

echo "## largest unfragmented ICMP payload over wg0 (DF set)"
for size in 1392 1352 1300 1200 1172; do
  if vmssh "ping -c1 -W1 -M do -s $size 10.99.0.21 >/dev/null 2>&1"; then
    echo "payload $size ok (packet $((size + 28)))"; break
  else
    echo "payload $size dropped"
  fi
done

echo "## iperf3 gpu0 -> cpu1 over wg (10s)"
cexec sh -c 'iperf3 -s -1 -D'
sleep 1
vmssh 'iperf3 -c 10.99.0.21 -t 10 2>&1 | tail -4'
echo "## iperf3 cpu1 -> gpu0 over wg (10s, reverse)"
cexec sh -c 'iperf3 -s -1 -D'
sleep 1
vmssh 'iperf3 -c 10.99.0.21 -t 10 -R 2>&1 | tail -4'
