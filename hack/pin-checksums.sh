#!/usr/bin/env bash
# Resolve upstream-published sha256 sums for the artifacts baked into node
# images, for the versions in versions.env, and print versions.env lines.
# usage: hack/pin-checksums.sh   (then paste the output into versions.env)
set -euo pipefail
cd "$(dirname "$0")/.."
set -a; source versions.env; set +a

# Both helpers exit non-zero on download failure or a missing entry, so a
# network hiccup can never produce an empty pin.
sum_from() { # sum_from <url-of-sums-file> <filename>
  local body h
  body=$(curl -fsSL "$1") || { echo "download failed: $1" >&2; exit 1; }
  h=$(awk -v f="$2" '$2==f || $2=="*"f || $2=="./"f {print $1; exit}' <<<"$body")
  [[ $h =~ ^[0-9a-f]{64}$ ]] || { echo "no sha256 for $2 in $1" >&2; exit 1; }
  echo "$h"
}
one() { # one <url-of-.sha256-file>  (file contains just the hash, maybe + name)
  local h
  h=$(curl -fsSL "$1" | awk '{print $1; exit}') || true
  [[ $h =~ ^[0-9a-f]{64}$ ]] || { echo "no sha256 at $1" >&2; exit 1; }
  echo "$h"
}

# pin NAME FUNC ARGS...: assign first so `set -e` aborts on failure; an
# `echo "X=$(f)"` would print an empty pin instead.
pin() {
  local name=$1 v
  shift
  v=$("$@")
  echo "$name=$v"
}

k3s_v=$(printf %s "$K3S_VERSION" | sed 's/+/%2B/')
k3s_sums="https://github.com/k3s-io/k3s/releases/download/$k3s_v/sha256sum-arm64.txt"
pin K3S_SHA256_ARM64 sum_from "$k3s_sums" k3s-arm64
pin K3S_AIRGAP_SHA256_ARM64 sum_from "$k3s_sums" k3s-airgap-images-arm64.tar.zst
for b in kubeadm kubelet kubectl; do
  pin "$(echo "$b" | tr a-z A-Z)_SHA256_ARM64" one "https://dl.k8s.io/release/$KUBERNETES_VERSION/bin/linux/arm64/$b.sha256"
done
pin CONTAINERD_SHA256_ARM64 one "https://github.com/containerd/containerd/releases/download/$CONTAINERD_VERSION/containerd-${CONTAINERD_VERSION#v}-linux-arm64.tar.gz.sha256sum"
pin RUNC_SHA256_ARM64 sum_from "https://github.com/opencontainers/runc/releases/download/$RUNC_VERSION/runc.sha256sum" runc.arm64
pin CNI_PLUGINS_SHA256_ARM64 one "https://github.com/containernetworking/plugins/releases/download/$CNI_PLUGINS_VERSION/cni-plugins-linux-arm64-$CNI_PLUGINS_VERSION.tgz.sha256"
pin CRICTL_SHA256_ARM64 one "https://github.com/kubernetes-sigs/cri-tools/releases/download/$CRICTL_VERSION/crictl-$CRICTL_VERSION-linux-arm64.tar.gz.sha256"
pin ETCD_SHA256_ARM64 sum_from "https://github.com/etcd-io/etcd/releases/download/$ETCD_VERSION/SHA256SUMS" "etcd-$ETCD_VERSION-linux-arm64.tar.gz"
