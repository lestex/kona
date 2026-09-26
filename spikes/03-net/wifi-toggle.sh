#!/usr/bin/env bash
# Wi-Fi change test: with the mesh up, turn Wi-Fi off and on and check that
# vmnet bridges, static addresses, published ports and the WireGuard path
# survive. Disrupts host networking for ~15s; run only with consent.
set -uo pipefail
source "$(dirname "$0")/lib.sh"
CPU=cpu1
wifi=${WIFI_IF:-en0}

check() {
  echo "-- $1 ($(date +%T))"
  ifconfig | grep -E '^bridge10[0-9]' | cut -d: -f1 | tr '\n' ' '; echo
  echo "host -> gpu0 $GPU_IP: $(ping -c1 -t2 "$GPU_IP" >/dev/null && echo ok || echo FAIL)"
  echo "host -> cpu1 static $CPU_STATIC: $(ping -c1 -t2 "$CPU_STATIC" >/dev/null && echo ok || echo FAIL)"
  echo "gpu0 -> cpu1 over wg: $(vmssh 'ping -c2 -W2 10.99.0.21 >/dev/null' && echo ok || echo FAIL)"
  echo "cpu1 -> gpu0 over wg: $(cexec ping -c2 -W2 10.99.0.10 >/dev/null && echo ok || echo FAIL)"
  echo "cpu1 handshake: $(echo $(( $(date +%s) - $(cexec wg show wg0 latest-handshakes | awk '{print $2}') ))s ago)"
  echo "cpu1 -> internet: $(cexec ping -c1 -W2 1.1.1.1 >/dev/null && echo ok || echo FAIL)"
}

check "before"
echo "== Wi-Fi off"
networksetup -setairportpower "$wifi" off
sleep 5
check "Wi-Fi off"
echo "== Wi-Fi on"
networksetup -setairportpower "$wifi" on
for _ in $(seq 30); do
  route -n get default >/dev/null 2>&1 && ping -c1 -t1 1.1.1.1 >/dev/null 2>&1 && break
  sleep 1
done
back=$(date +%s)
check "Wi-Fi back"
echo "== waiting for gpu0 -> cpu1 over wg to recover"
for _ in $(seq 60); do
  if vmssh 'ping -c1 -W1 10.99.0.21 >/dev/null'; then
    echo "recovered $(( $(date +%s) - back ))s after Wi-Fi came back"
    break
  fi
  sleep 1
done
check "recovered"
