#!/bin/bash
# Runs once inside the GPU node image during `make -C images/gpu-node`, via
# cloud-init. Installs everything a kona GPU worker needs, then resets
# cloud-init so each node's own seed applies on its first boot.
# The full log goes to /var/log/kona-provision.log; only the result and
# the package manifest are written to the serial console (hvc0), which
# build.sh reads back. (Streaming to hvc0 with tee breaks: getty's vhangup
# on the console kills the writer and the script with it.)
set -euxo pipefail
exec >/var/log/kona-provision.log 2>&1

: "${K3S_VERSION:?}" "${K3S_SHA256:?}" "${K3S_AIRGAP_SHA256:?}" "${MESA_VERSION:?}"

dnf -y install --setopt=install_weak_deps=False \
  vulkan-tools wireguard-tools chrony iptables-nft conntrack-tools iproute \
  ethtool socat tar zstd dnf-plugins-core python3-dnf-plugin-versionlock

# Upstream Fedora Mesa fails vkCreateInstance under krunkit; the libkrun
# maintainers' patched build works (docs/gpu.md). Pin and lock it.
dnf -y copr enable slp/mesa-libkrun-vulkan
dnf -y install --allow-downgrade --setopt=install_weak_deps=False \
  "mesa-vulkan-drivers-$MESA_VERSION" "mesa-filesystem-$MESA_VERSION"
dnf versionlock add "mesa-vulkan-drivers-$MESA_VERSION" "mesa-filesystem-$MESA_VERSION"
# Only the Venus ICD is useful here; others just add loader warnings.
find /usr/share/vulkan/icd.d -name '*.json' ! -name 'virtio_icd.*' ! -name 'lvp_icd.*' -delete

v=$(printf %s "$K3S_VERSION" | sed 's/+/%2B/')
base=https://github.com/k3s-io/k3s/releases/download/$v
curl -fsSL -o /usr/local/bin/k3s "$base/k3s-arm64"
echo "$K3S_SHA256  /usr/local/bin/k3s" | sha256sum -c -
chmod 0755 /usr/local/bin/k3s
for l in kubectl crictl ctr; do ln -sf k3s /usr/local/bin/$l; done
mkdir -p /var/lib/rancher/k3s/agent/images
curl -fsSL -o /var/lib/rancher/k3s/agent/images/k3s-airgap-images-arm64.tar.zst "$base/k3s-airgap-images-arm64.tar.zst"
echo "$K3S_AIRGAP_SHA256  /var/lib/rancher/k3s/agent/images/k3s-airgap-images-arm64.tar.zst" | sha256sum -c -

install -m 0755 /run/kona-build/kona-wg /usr/local/bin/kona-wg
install -m 0644 /run/kona-build/kona-wg.service /run/kona-build/kona-agent.service /etc/systemd/system/
mkdir -p /etc/kona && chmod 0700 /etc/kona

# Guest clocks stop while the host sleeps: step whenever >1s off.
sed -i 's/^makestep .*/makestep 1 -1/' /etc/chrony.conf
systemctl enable chronyd

# k3s would need the k3s-selinux policy, which is not packaged for Fedora 44.
sed -i 's/^SELINUX=.*/SELINUX=permissive/' /etc/selinux/config

cat > /etc/sysctl.d/90-kona.conf <<'EOF'
net.ipv4.ip_forward = 1
fs.inotify.max_user_instances = 1024
fs.inotify.max_user_watches = 524288
EOF

rpm -qa --qf '%{NAME}-%{VERSION}-%{RELEASE}.%{ARCH}\n' | sort > /run/kona-build/manifest

dnf clean all
rm -f /etc/ssh/ssh_host_*
cloud-init clean --logs --seed --machine-id
systemctl unmask serial-getty@hvc0.service
{ echo KONA-MANIFEST-BEGIN; cat /run/kona-build/manifest; echo KONA-MANIFEST-END; echo KONA-PROVISION-OK; } > /dev/hvc0
sync
