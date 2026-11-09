# Clusters

```sh
kona create cluster [NAME] [flags]
```

`NAME` defaults to `kona`. Every other command takes the cluster as
`[NAME]` or `--name NAME` and uses the same default, so with a single
cluster you rarely need to type it.

## Size

| Flag | Default | |
|---|---|---|
| `--workers` | `2` | Worker nodes. With `0`, a k3s control plane runs your pods too. A kubeadm control plane keeps its `NoSchedule` taint, so pods without a toleration stay Pending. |
| `--control-planes` | `1` | Control-plane nodes. Use an odd number; more than one runs etcd across them (see [High availability](#high-availability)). |
| `--cpus` | `2` | vCPUs per node. |
| `--memory` | `2G` | Memory per node, such as `4G` or `1536M`. |

Every node is a VM with **fixed** memory: it holds all of `--memory` from
boot to stop and doesn't give unused memory back to macOS. kona warns when
the nodes together would take more than 75% of the Mac's memory. Stop
clusters you're not using.

Add a worker to a running cluster at any time:

```sh
kona add node [--name NAME] [--cpus 4] [--memory 4G]
```

## Distribution

`--distro k3s` (the default) or `--distro kubeadm`. k3s starts in seconds
and uses less memory. kubeadm builds the cluster the way production
clusters are built, with static pods, a separate etcd and upstream
components. [k3s or kubeadm](../distros.md) compares them.

## Network plugin

`--cni default` uses flannel (VXLAN) on both distributions. `--cni cilium`
installs Cilium instead, with kube-proxy replacement and Hubble.

```sh
kona create cluster --cni cilium
```

Cilium needs the tc/eBPF datapath that `container`'s default kernel lacks,
which is the main reason kona boots its own [guest kernel](../kernel.md).

## High availability

```sh
kona create cluster --control-planes 3
```

On k3s, the control planes run embedded etcd. On kubeadm, they run stacked
etcd static pods. Either way the cluster keeps working with one control
plane down, and `kona stop node` refuses to stop so many that etcd would
lose quorum.

The API endpoint in the kubeconfig is the first control plane's address.
There is no load balancer in front of the control planes, so `kubectl`
loses the API while that one is down, even though the cluster itself
keeps running.

## Addresses

Each cluster gets its own `container` network: the first free `/24` from
`192.168.70.0/24` to `192.168.99.0/24`. Inside it, addresses are fixed:

| Address | Node |
|---|---|
| `.10`, `.11`, … | control planes |
| `.20`, `.21`, … | workers |
| `.200` | the cluster's image registry, once you [load an image](../registry.md) |

Nodes keep their addresses across stop and start. The Mac reaches all of
them directly. [Networking](../networking.md) explains the design.

## DNS

Nodes and CoreDNS forward to the Mac's first IPv4 resolver, read from
`scutil --dns` at create time. Pass `--dns` to use another server. It must
be IPv4, because pods are IPv4-only and can't reach an IPv6 resolver.

## Versions and images

Each kona release pins one Kubernetes version per distribution, and its
node images on `ghcr.io/lestex/kona-node`. `kona version` lists the pins.
`--image` runs a different node image, for example one you built from
`images/node`. `--kernel` boots a different guest kernel.

## Failures

`create` waits up to `--wait` (default 5m) for the nodes to be Ready and
the cluster to pass `kona verify cluster`. If anything fails, it deletes
everything it created. Use `--retain` to keep the VMs and state for
debugging. Running the same `kona create cluster NAME` again then resumes
from where it stopped.

## Several clusters

Clusters are independent, each with its own network, nodes and kubeconfig
(`~/.kube/kona-<name>`):

```sh
kona create cluster dev
kona create cluster ha --control-planes 3 --distro kubeadm
kona get clusters
```

## Deleting

```sh
kona delete cluster [NAME]
kona delete cluster --all
```

`delete` removes the VMs, volumes, network, kubeconfig (and the
`kona-<name>` context, if it was merged) and state. It is idempotent. `--all`
also removes orphans: `container` resources labeled with a kona cluster
that kona no longer has state for.
