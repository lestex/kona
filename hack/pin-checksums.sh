#!/usr/bin/env bash
# Resolve upstream-published sha256 sums for the artifacts baked into node
# images, for the versions in versions.env, and print versions.env lines.
# usage: hack/pin-checksums.sh   (then paste the output into versions.env)
set -euo pipefail
cd "$(dirname "$0")/.."
set -a; source versions.env; set +a

sum_from() { # sum_from <url-of-sums-file> <filename>
  curl -fsSL "$1" | awk -v f="$2" '$2==f || $2=="*"f || $2=="./"f {print $1; found=1} END{exit !found}'
}
one() { # one <url-of-.sha256-file>  (file contains just the hash, maybe + name)
  curl -fsSL "$1" | awk '{print $1; exit}'
}

k3s_v=$(printf %s "$K3S_VERSION" | sed 's/+/%2B/')
echo "K3S_SHA256_ARM64=$(sum_from "https://github.com/k3s-io/k3s/releases/download/$k3s_v/sha256sum-arm64.txt" k3s-arm64)"
echo "K3S_AIRGAP_SHA256_ARM64=$(sum_from "https://github.com/k3s-io/k3s/releases/download/$k3s_v/sha256sum-arm64.txt" k3s-airgap-images-arm64.tar.zst)"
for b in kubeadm kubelet kubectl; do
  up=$(echo "$b" | tr a-z A-Z)
  echo "${up}_SHA256_ARM64=$(one "https://dl.k8s.io/release/$KUBERNETES_VERSION/bin/linux/arm64/$b.sha256")"
done
cv=${CONTAINERD_VERSION#v}
echo "CONTAINERD_SHA256_ARM64=$(one "https://github.com/containerd/containerd/releases/download/$CONTAINERD_VERSION/containerd-$cv-linux-arm64.tar.gz.sha256sum")"
echo "RUNC_SHA256_ARM64=$(sum_from "https://github.com/opencontainers/runc/releases/download/$RUNC_VERSION/runc.sha256sum" runc.arm64)"
echo "CNI_PLUGINS_SHA256_ARM64=$(one "https://github.com/containernetworking/plugins/releases/download/$CNI_PLUGINS_VERSION/cni-plugins-linux-arm64-$CNI_PLUGINS_VERSION.tgz.sha256")"
echo "CRICTL_SHA256_ARM64=$(one "https://github.com/kubernetes-sigs/cri-tools/releases/download/$CRICTL_VERSION/crictl-$CRICTL_VERSION-linux-arm64.tar.gz.sha256")"
echo "ETCD_SHA256_ARM64=$(sum_from "https://github.com/etcd-io/etcd/releases/download/$ETCD_VERSION/SHA256SUMS" "etcd-$ETCD_VERSION-linux-arm64.tar.gz")"
