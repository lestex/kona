---
title: kona
layout: hextra-home
# Every page uses Hextra's docs layout (sidebar, table of contents); the
# pages sit at the site root rather than under /docs/, where Hextra would
# infer it from the folder name.
cascade:
  - type: docs
---

{{< hextra/hero-badge link="https://github.com/lestex/kona/releases" >}}
  Apple silicon · macOS 26+
{{< /hextra/hero-badge >}}

<div class="hx:mt-6 hx:mb-6">
{{< hextra/hero-headline >}}
  Kubernetes on Apple silicon,&nbsp;<br class="hx:sm:block hx:hidden" />one VM per node
{{< /hextra/hero-headline >}}
</div>

<div class="hx:mb-12">
{{< hextra/hero-subtitle >}}
  Clusters from Apple&nbsp;<code>container</code>&nbsp;VMs: every node has its own kernel,&nbsp;<br class="hx:sm:block hx:hidden" />address and disk, on a network your Mac reaches directly.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx:mb-6">
{{< hextra/hero-button text="Get started" link="getting-started/installation/" >}}
</div>

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

<div class="hx:mt-6"></div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="k3s or kubeadm"
    subtitle="k3s by default: three nodes up in about 13 seconds. kubeadm for the upstream layout, with static pods and a separate etcd."
    link="guide/distros/"
  >}}
  {{< hextra/feature-card
    title="Flannel or Cilium"
    subtitle="`--cni cilium` runs Cilium with kube-proxy replacement, on a kona kernel built with its tc/eBPF datapath."
    link="design/kernel/"
  >}}
  {{< hextra/feature-card
    title="High availability"
    subtitle="`--control-planes 3` runs etcd across three control-plane VMs."
    link="guide/clusters/#high-availability"
  >}}
  {{< hextra/feature-card
    title="Your own images"
    subtitle="`kona load image myapp:dev` pushes an image from your Mac into the cluster; pods pull it by its usual name."
    link="guide/registry/"
  >}}
  {{< hextra/feature-card
    title="Survives reboots"
    subtitle="`kona stop cluster` and `kona start cluster` keep VMs and disks, and start waits until the cluster is healthy."
    link="guide/lifecycle/"
  >}}
  {{< hextra/feature-card
    title="Built-in checks"
    subtitle="`kona doctor` checks the Mac, `kona verify cluster` a cluster, and `kona support bundle` collects what's needed to debug one."
    link="guide/troubleshooting/"
  >}}
{{< /hextra/feature-grid >}}

## Requirements

- A Mac with Apple silicon.
- macOS 26 (Tahoe) or later. On macOS 15, `container` VMs on the same
  network can't reach each other.
- [Apple `container`](https://github.com/apple/container) 1.7.1 or later,
  which the Homebrew cask installs.
- Enough memory for the nodes: each VM has fixed memory (2G by default),
  and VMs don't give unused memory back to macOS.
