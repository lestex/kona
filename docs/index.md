# kona

**Kubernetes on Apple silicon, with one lightweight VM per node.**

kona creates Kubernetes clusters on a Mac from
[Apple `container`](https://github.com/apple/container) VMs. Every node is
its own Linux VM, with its own kernel, IP address and disk, not a container
inside a shared VM. So a cluster behaves like a small real one: nodes boot,
stop and fail independently, and each has a stable address on a private
network the Mac can reach directly.

```sh
brew install lestex/tap/kona
kona kernel install
kona create cluster
kubectl --kubeconfig ~/.kube/kona-kona get nodes
```

```text
NAME                   STATUS   ROLES           AGE   VERSION
kona-control-plane-1   Ready    control-plane   14s   v1.37.0+k3s1
kona-worker-1          Ready    <none>          9s    v1.37.0+k3s1
kona-worker-2          Ready    <none>          9s    v1.37.0+k3s1
```

## What you get

- **k3s or kubeadm.** k3s by default: a three-node cluster is up in about
  13 seconds. kubeadm when you want the upstream layout, with static pods
  and a separate etcd. See [k3s or kubeadm](distros.md).
- **Flannel or Cilium.** `--cni cilium` installs Cilium with kube-proxy
  replacement, on a kona kernel built with Cilium's tc/eBPF datapath.
- **High availability.** `--control-planes 3` runs embedded etcd across
  three control-plane VMs.
- **Your own images.** `kona load image myapp:dev` pushes an image from
  your Mac into the cluster, and pods pull it by its usual name. See
  [Loading images](registry.md).
- **A lifecycle that survives reboots.** `kona stop cluster` and
  `kona start cluster` keep VMs and disks, and `start` waits until the
  cluster is actually healthy. See [Stop, start, verify](lifecycle.md).
- **Built-in checks.** `kona doctor` checks the Mac, `kona verify cluster`
  checks a cluster, and `kona support bundle` collects everything needed
  to debug one.

## Requirements

- A Mac with Apple silicon.
- macOS 26 (Tahoe) or later. On macOS 15, `container` VMs on the same
  network can't reach each other.
- [Apple `container`](https://github.com/apple/container) 1.7.1 or later,
  which the Homebrew cask installs.
- Enough memory for the nodes: each VM has fixed memory (2G by default),
  and VMs don't give unused memory back to macOS.

[Install kona](getting-started/installation.md){ .md-button .md-button--primary }
[Quick start](getting-started/quick-start.md){ .md-button }
