#!/usr/bin/env bash
# Build the kona GPU node disk image (krunkit + Venus), reproducibly from
# pinned inputs: Fedora Cloud base (sha256), k3s (sha256), patched Mesa NVR.
# Other Fedora packages resolve from the Fedora 44 repos at build time; the
# exact set is recorded in out/<image>.packages.
#
# Steps: verify base qcow2 -> convert to raw in a `container` VM (qemu-img)
# -> grow to DISK_SIZE -> boot once under krunkit with a provisioning
# cloud-init seed -> check the console for success -> write sha256.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$here/../..
set -a; source "$root/versions.env"; set +a

out=$here/out
tag=$(printf %s "$K3S_VERSION" | tr + -)
name=kona-gpu-node-$tag
raw=$out/$name.raw
disk_size=${DISK_SIZE:-20G}
dns=$(scutil --dns | awk '/nameserver\[0\]/{print $3; exit}')
subnet_base=${BUILD_SUBNET:-192.168.249}
work=$out/work
run=/tmp/kona-$(id -u)/gpu-build   # unix sockets: 103-byte path limit
helper=$(brew --prefix vmnet-helper)/libexec/vmnet-helper

mkdir -p "$work" "$run"
chmod 700 "$(dirname "$run")"

log() { echo "==> $*" >&2; }

# 1. Base image.
base=$work/$FEDORA_CLOUD_IMAGE
if [ ! -f "$base" ]; then
  log "downloading $FEDORA_CLOUD_IMAGE"
  curl -fL -o "$base.part" \
    "https://dl.fedoraproject.org/pub/fedora/linux/releases/$FEDORA_VERSION/Cloud/aarch64/images/$FEDORA_CLOUD_IMAGE"
  mv "$base.part" "$base"
fi
echo "$FEDORA_CLOUD_SHA256_ARM64  $base" | shasum -a 256 -c - >/dev/null
log "base image verified"

# 2. qcow2 -> raw inside a throwaway container VM (no qemu on the host).
log "converting to raw"
rm -f "$raw"
container run --rm --progress none --dns "$dns" -v "$work:/work" -v "$out:/out" \
  "docker.io/library/fedora:$FEDORA_VERSION@$FEDORA_DIGEST" sh -c \
  "dnf -q -y install qemu-img >/dev/null && qemu-img convert -p -O raw /work/$FEDORA_CLOUD_IMAGE /out/$name.raw"
# Grow the (sparse) file; cloud-init's growpart expands the root partition.
dd if=/dev/zero of="$raw" bs=1 count=0 seek="$disk_size" 2>/dev/null

# 3. Provisioning seed.
seed=$work/seed
rm -rf "$seed" && mkdir -p "$seed"
b64() { base64 < "$1" | tr -d '\n'; }
cat > "$seed/meta-data" <<EOF
instance-id: kona-gpu-build-$(date +%s)
local-hostname: kona-gpu-build
EOF
# DHCP for the address, but never the vmnet gateway as DNS: its forwarder
# refuses queries on some hosts (docs/failure-modes.md).
cat > "$seed/network-config" <<EOF
version: 2
ethernets:
  eth0:
    match: {macaddress: "52:54:00:f9:00:01"}
    set-name: eth0
    dhcp4: true
    dhcp4-overrides: {use-dns: false}
    nameservers: {addresses: [$dns]}
EOF
cat > "$seed/user-data" <<EOF
#cloud-config
# Keep getty off the console while provisioning writes results to it.
bootcmd:
  - [systemctl, mask, --now, serial-getty@hvc0.service]
write_files:
  - {path: /run/kona-build/provision.sh, permissions: '0755', encoding: b64, content: $(b64 "$here/provision.sh")}
  - {path: /run/kona-build/kona-wg, permissions: '0755', encoding: b64, content: $(b64 "$root/images/node/kona-wg")}
  - {path: /run/kona-build/kona-wg.service, encoding: b64, content: $(b64 "$here/files/kona-wg.service")}
  - {path: /run/kona-build/kona-agent.service, encoding: b64, content: $(b64 "$here/files/kona-agent.service")}
runcmd:
  - [sh, -c, "K3S_VERSION='$K3S_VERSION' K3S_SHA256='$K3S_SHA256_ARM64' K3S_AIRGAP_SHA256='$K3S_AIRGAP_SHA256_ARM64' MESA_VERSION='$MESA_VERSION' /run/kona-build/provision.sh && poweroff || { { echo KONA-PROVISION-FAILED; tail -40 /var/log/kona-provision.log; } > /dev/hvc0; poweroff; }"]
EOF
rm -f "$work/seed.iso"
hdiutil makehybrid -quiet -iso -joliet -default-volume-name cidata -o "$work/seed.iso" "$seed"

# 4. Boot once under krunkit on a private vmnet network (DHCP) and wait for
# the provisioning run to power the VM off.
console=$work/console.log
: > "$console"
sock=$run/build.sock
rm -f "$sock"
"$helper" --socket "$sock" --interface-id "$(uuidgen)" --operation-mode shared \
  --start-address "$subnet_base.1" --end-address "$subnet_base.254" --subnet-mask 255.255.255.0 \
  </dev/null >"$work/helper.json" 2>"$work/helper.log" &
hpid=$!
trap 'kill -9 $hpid 2>/dev/null || true' EXIT
for _ in $(seq 50); do [ -s "$work/helper.json" ] && break; sleep 0.2; done
[ -s "$work/helper.json" ] || { log "vmnet-helper did not start (subnet $subnet_base.0/24 in use?)"; exit 1; }

log "provisioning under krunkit (console: $console)"
krunkit --cpus 4 --memory 4096 \
  --device "virtio-blk,path=$raw,format=raw" \
  --device "virtio-blk,path=$work/seed.iso,format=raw" \
  --device "virtio-net,type=unixgram,path=$sock,mac=52:54:00:f9:00:01" \
  --device "virtio-serial,logFilePath=$console" \
  </dev/null >"$work/krunkit.log" 2>&1 &
kpid=$!
timeout=${PROVISION_TIMEOUT:-1800}
for _ in $(seq "$timeout"); do kill -0 $kpid 2>/dev/null || break; sleep 1; done
if kill -0 $kpid 2>/dev/null; then
  kill -9 $kpid
  log "provisioning timed out after ${timeout}s; see $console"
  exit 1
fi

if ! grep -q KONA-PROVISION-OK "$console"; then
  log "provisioning failed; see $console"
  exit 1
fi
tr -d '\r' < "$console" | sed -n '/^KONA-MANIFEST-BEGIN/,/^KONA-MANIFEST-END/p' | sed '1d;$d' > "$out/$name.packages"
(cd "$out" && shasum -a 256 "$name.raw" > "$name.raw.sha256")
log "built $raw ($(wc -l < "$out/$name.packages" | tr -d ' ') packages)"
cat "$out/$name.raw.sha256"
