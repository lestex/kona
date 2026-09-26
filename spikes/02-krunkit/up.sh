#!/usr/bin/env bash
# Phase 0 spike: boot one krunkit VM (Fedora Cloud) on its own vmnet-helper
# network with a pinned MAC and static IP, then wait for SSH.
# usage: spikes/02-krunkit/up.sh [name] [ip] [mac]
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
work=$here/work
name=${1:-gpu0}
ip=${2:-192.168.65.10}
mac=${3:-52:54:00:6b:00:10}
subnet_start=192.168.65.1
subnet_end=192.168.65.254
gw=192.168.65.1
image=$work/Fedora-Cloud-Base-Generic-44-1.7.aarch64.qcow2
helper=$(brew --prefix vmnet-helper)/libexec/vmnet-helper
# unix socket paths are limited to 103 bytes, keep them short
run=/tmp/kona-$(id -u)/spike
vm=$work/$name
dns=$(scutil --dns | awk '/nameserver\[0\]/{print $3; exit}')
mesa=$(sed -n 's/^MESA_VERSION=//p' "$here/../../versions.env")

mkdir -p "$run" "$vm"
chmod 700 "$(dirname "$run")"
[ -f "$work/id_ed25519" ] || ssh-keygen -q -t ed25519 -N '' -f "$work/id_ed25519"

# Root disk: APFS clone of the pristine image, grown so dnf has room.
if [ ! -f "$vm/disk.qcow2" ]; then
  cp -c "$image" "$vm/disk.qcow2"
fi

# cloud-init NoCloud seed.
seed=$vm/seed
mkdir -p "$seed"
cat > "$seed/meta-data" <<EOF
instance-id: $name
local-hostname: $name
EOF
cat > "$seed/network-config" <<EOF
version: 2
ethernets:
  eth0:
    match: {macaddress: "$mac"}
    set-name: eth0
    addresses: [$ip/24]
    routes: [{to: default, via: $gw}]
    nameservers: {addresses: [$dns]}
    mtu: 1500
EOF
cat > "$seed/user-data" <<EOF
#cloud-config
users:
  - name: kona
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    ssh_authorized_keys: ["$(cat "$work/id_ed25519.pub")"]
packages: [vulkan-tools, chrony, iperf3, tcpdump, iproute, dnf-plugins-core]
runcmd:
  - [systemctl, enable, --now, chronyd]
  # Upstream Fedora Mesa fails vkCreateInstance under krunkit (host rejects
  # RESOURCE_MAP_BLOB). The libkrun maintainers' patched Mesa works.
  - [dnf, -y, copr, enable, slp/mesa-libkrun-vulkan]
  - [dnf, -y, install, --allow-downgrade, "mesa-vulkan-drivers-$mesa", "mesa-filesystem-$mesa"]
EOF
rm -f "$vm/seed.iso"
hdiutil makehybrid -quiet -iso -joliet -default-volume-name cidata -o "$vm/seed.iso" "$seed"

# Network: dedicated vmnet shared network for krunkit nodes. It cannot be
# the same subnet as the `container` network (vmnet refuses), see
# docs/networking.md.
sock=$run/$name.sock
rm -f "$sock"
"$helper" --socket "$sock" --interface-id "$(uuidgen)" \
  --operation-mode shared --start-address $subnet_start --end-address $subnet_end \
  --subnet-mask 255.255.255.0 >"$vm/helper.json" 2>"$vm/helper.log" </dev/null &
echo $! >"$vm/helper.pid"
for _ in $(seq 50); do [ -s "$vm/helper.json" ] && break; sleep 0.2; done
if [ ! -s "$vm/helper.json" ]; then
  echo "vmnet-helper did not start within 10s" >&2
  kill -9 "$(cat "$vm/helper.pid")"
  exit 1
fi

krunkit --cpus 4 --memory 4096 \
  --device "virtio-blk,path=$vm/disk.qcow2,format=qcow2" \
  --device "virtio-blk,path=$vm/seed.iso,format=raw" \
  --device "virtio-net,type=unixgram,path=$sock,mac=$mac" \
  --device "virtio-serial,logFilePath=$vm/console.log" \
  --restful-uri "unix://$run/$name.api" \
  --pidfile "$vm/krunkit.pid" \
  --log-file "$vm/krunkit.log" </dev/null >>"$vm/krunkit.log" 2>&1 &

ssh_opts=(-i "$work/id_ed25519" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=3 -o LogLevel=ERROR)
for _ in $(seq 120); do
  if ssh "${ssh_opts[@]}" kona@"$ip" true 2>/dev/null; then
    echo "$name up at $ip, waiting for cloud-init (packages + patched Mesa)"
    ssh "${ssh_opts[@]}" kona@"$ip" 'cloud-init status --wait >/dev/null; cloud-init status; rpm -q mesa-vulkan-drivers'
    exit 0
  fi
  sleep 2
done
echo "$name did not come up; see $vm/console.log" >&2
exit 1
