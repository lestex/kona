#!/usr/bin/env bash
# Remove everything the Phase 0 spikes create. Safe to run repeatedly.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)

"$here/02-krunkit/down.sh" gpu0
for c in cpu0 cpu1; do container rm -f "$c" >/dev/null 2>&1 && echo "removed $c"; done
container network rm kona-spike >/dev/null 2>&1 && echo "removed network kona-spike"
# Helpers that hung in vmnet_start_interface ignore SIGTERM.
pkill -9 -f "vmnet-helper --socket /tmp/kona-$(id -u)/" 2>/dev/null && echo "killed stray vmnet-helpers"
rm -rf "/tmp/kona-$(id -u)/spike" "/tmp/kona-$(id -u)"/*.sock "/tmp/kona-$(id -u)"/*.log
echo "VM disks kept in spikes/02-krunkit/work (rm -rf it to reclaim ~2 GB)"
