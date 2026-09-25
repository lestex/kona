#!/usr/bin/env bash
# Runs inside the kernel builder VM. Merges kona.config onto the base config,
# builds an arm64 Image, and fails if any requested option did not stick.
# Inputs (mounted at /kernel): base-arm64.config, kona.config, source tarball.
set -euo pipefail

: "${KERNEL_VERSION:?}"
out=/kernel/out
mkdir -p "$out" /kbuild
tar -xf "/kernel/out/linux-${KERNEL_VERSION}.tar.xz" -C /kbuild --strip-components=1
cd /kbuild

cp /kernel/base-arm64.config .config
scripts/kconfig/merge_config.sh -m .config /kernel/kona.config
make ARCH=arm64 olddefconfig

# Every CONFIG_X=y in the fragment must survive olddefconfig. A dropped option
# means an unmet dependency, so fail with its name.
missing=0
while IFS= read -r line; do
  opt=${line%%=*}
  if ! grep -qx "$line" .config; then
    echo "missing after olddefconfig: $opt (want ${line#*=})" >&2
    missing=1
  fi
done < <(grep -E '^CONFIG_[A-Z0-9_]+=' /kernel/kona.config)
[ "$missing" -eq 0 ] || exit 1

make ARCH=arm64 -j"$(nproc)" LOCALVERSION=-kona Image
cp arch/arm64/boot/Image "$out/vmlinux-${KERNEL_VERSION}-kona"
cp .config "$out/config-${KERNEL_VERSION}-kona"
sha256sum "$out/vmlinux-${KERNEL_VERSION}-kona" | tee "$out/vmlinux-${KERNEL_VERSION}-kona.sha256"
