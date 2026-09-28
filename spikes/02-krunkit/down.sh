#!/usr/bin/env bash
# Stop a spike krunkit VM and its vmnet-helper.
# usage: spikes/02-krunkit/down.sh [name]
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
name=${1:-gpu0}
vm=$here/work/$name
api=/tmp/kona-$(id -u)/spike/$name.api

curl -s --unix-socket "$api" -X POST http://localhost/vm/state -d '{"state":"Stop"}' >/dev/null 2>&1
for f in krunkit.pid helper.pid; do
  [ -f "$vm/$f" ] || continue
  pid=$(cat "$vm/$f")
  kill "$pid" 2>/dev/null
  for _ in $(seq 25); do kill -0 "$pid" 2>/dev/null || break; sleep 0.2; done
  kill -9 "$pid" 2>/dev/null
  rm -f "$vm/$f"
done
rm -f "$api" "/tmp/kona-$(id -u)/spike/$name.sock"
echo "$name down"
