# Shared helpers for the networking spike. Source from bash.
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
key=$here/../02-krunkit/work/id_ed25519
GPU_IP=${GPU_IP:-192.168.65.10}
CPU=${CPU:-cpu0}
CPU_STATIC=${CPU_STATIC:-192.168.66.200}

vmssh() {
  ssh -i "$key" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -o LogLevel=ERROR -o ConnectTimeout=5 "kona@$GPU_IP" "$@"
}

cexec() {
  container exec "$CPU" "$@"
}

cpu_ip() {
  container exec "$CPU" sh -c "ip -4 -o addr show eth0 | awk '{print \$4}' | cut -d/ -f1 | head -1"
}

# Host bridge that owns gateway address $1 (numbering is not stable).
bridge_for() {
  ifconfig | awk -v gw="$1" '/^[a-z]/{i=$1; sub(":","",i)} $1=="inet" && $2==gw {print i}'
}
