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

# Reproducible: the same source, config and toolchain image give the same
# Image on any machine, so CI can publish the kernel versions.env pins
# (docs/design/kernel.md). These replace the build host, user and time the kernel
# would otherwise stamp into `uname -v` and /proc/version.
export KBUILD_BUILD_TIMESTAMP="1970-01-01 00:00:00 UTC" \
       KBUILD_BUILD_USER=kona KBUILD_BUILD_HOST=kona KBUILD_BUILD_VERSION=1

# BTF: for pahole 1.25 scripts/Makefile.btf passes -j<make jobs>, and
# parallel pahole emits BTF types in a different order on every run (its
# reproducible_build mode only arrived in 1.26). So pass Makefile.btf's
# 1.25 flags without -j: single-threaded encoding is deterministic. These
# flags are only right for 1.25, hence the check.
pahole_ver=$(pahole --version)
[ "$pahole_ver" = "v1.25" ] || { echo "PAHOLE_FLAGS below are for pahole v1.25, got $pahole_ver" >&2; exit 1; }
make ARCH=arm64 -j"$(nproc)" LOCALVERSION=-kona \
  PAHOLE_FLAGS="--btf_gen_floats --skip_encoding_btf_inconsistent_proto --btf_gen_optimized" Image
cp arch/arm64/boot/Image "$out/vmlinux-${KERNEL_VERSION}-kona"
cp .config "$out/config-${KERNEL_VERSION}-kona"
sha256sum "$out/vmlinux-${KERNEL_VERSION}-kona" | tee "$out/vmlinux-${KERNEL_VERSION}-kona.sha256"
