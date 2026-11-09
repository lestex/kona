# kona

Kubernetes on Apple silicon, with one lightweight VM per node.

kona creates Kubernetes clusters on a Mac from
[Apple `container`](https://github.com/apple/container) VMs. Every node is
its own Linux VM with its own kernel, address and disk, on a private
network the Mac reaches directly.

[![ci](https://github.com/lestex/kona/actions/workflows/ci.yml/badge.svg)](https://github.com/lestex/kona/actions/workflows/ci.yml)

**Documentation: [lestex.github.io/kona](https://lestex.github.io/kona/)**

## Install

Apple silicon and macOS 26 (Tahoe) or later.

```sh
brew install lestex/tap/kona     # not `brew install kona`: that's another project
container system start
kona kernel install
kona doctor
```

## Use

```sh
kona create cluster                          # k3s, 1 control plane + 2 workers
kubectl --kubeconfig ~/.kube/kona-kona get nodes

kona create cluster ha --distro kubeadm --control-planes 3 --cni cilium
kona load image myapp:dev                    # pods pull it by its usual name
kona stop cluster && kona start cluster
kona delete cluster
```

See the [quick start](https://lestex.github.io/kona/getting-started/quick-start/)
and the [command reference](https://lestex.github.io/kona/reference/cli/).

## Build

```sh
make build    # bin/kona
make check    # gofmt, vet, tests
```

[Building and testing](https://lestex.github.io/kona/development/) covers
the node images, the guest kernel and the docs site.
