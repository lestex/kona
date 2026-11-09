# Command line

Every command that acts on a cluster takes it as `[NAME]` or `--name NAME`,
defaulting to `kona`. `kona <command> --help` prints the same flags as
below.

## Clusters

### `kona create cluster [NAME]`

Create a cluster: one Apple `container` VM per node. See
[Clusters](../guide/clusters.md).

| Flag | Default | |
|---|---|---|
| `--distro` | `k3s` | `k3s` or `kubeadm` |
| `--cni` | `default` | `default` (flannel) or `cilium` |
| `--control-planes` | `1` | odd; more than one runs etcd across them |
| `--workers` | `2` | worker nodes |
| `--cpus` | `2` | vCPUs per node |
| `--memory` | `2G` | fixed memory per node |
| `--dns` | host's first IPv4 resolver | DNS server for nodes and CoreDNS |
| `--k8s-version` | the pinned one | must match a published node image |
| `--image` | `ghcr.io/lestex/kona-node:<version>` | node image |
| `--kernel` | the installed kona kernel | guest kernel |
| `--wait` | `5m` | how long to wait for the cluster to be ready |
| `--retain` | off | on failure, keep VMs and state instead of rolling back |

### `kona delete cluster [NAME]`

Delete a cluster's VMs, volumes, network, kubeconfig and state. Idempotent.

| Flag | |
|---|---|
| `--all` | delete every kona cluster, including orphans without state |

### `kona get clusters`

List clusters with phase, status (`ok`, `stopped`, `degraded`, `orphan`),
distribution, nodes and subnet. `-o json|yaml`.

### `kona get kubeconfig [--name NAME]`

Print the cluster's kubeconfig (context `kona-<name>`).

| Flag | |
|---|---|
| `--merge` | merge into `~/.kube/config` (or `$KUBECONFIG`) instead of printing |

### `kona stop cluster [NAME]`

Stop every node, workers first, keeping VMs, disks and state.

### `kona start cluster [NAME]`

Start a cluster's stopped nodes, control planes first, and wait until
`verify` passes. `--wait` (default `5m`).

### `kona verify cluster [NAME]`

Read-only health checks; exits 1 if any fails. See
[Checks](../lifecycle.md#checks).

| Flag | Default | |
|---|---|---|
| `-o` | table | `json` or `yaml` |
| `--timeout` | `2m` | give up after this long |

## Nodes

### `kona get nodes [--name NAME]`

List a cluster's nodes with role, IP, VM state, readiness, version and
clock skew. `-o json|yaml`.

### `kona add node [--name NAME]`

Add a worker and wait until it is Ready.

| Flag | Default |
|---|---|
| `--cpus` | `2` |
| `--memory` | `2G` |
| `--wait` | `5m` |

### `kona stop node NODE... [--name NAME]`

Stop the given nodes, keeping their VMs and disks. Refuses to stop control
planes when etcd would lose quorum.

### `kona start node NODE... [--name NAME]`

Start the given nodes and wait for them, and the cluster, to be healthy.
`--wait` (default `5m`).

### `kona ssh NODE [--name NAME] [-- COMMAND...]`

Open a shell on a node, or run a command there.

## Images

### `kona load image IMAGE... [--name NAME]`

Push images from the Mac's `container` image store into the cluster's
registry; pods pull them by their usual names. See
[Loading images](../registry.md).

## Host

### `kona doctor`

Check the Mac: macOS version, architecture, `container` version and
services, the guest kernel, DNS. Exits 1 if any check fails.

| Flag | |
|---|---|
| `--kernel` | check this kernel instead of the installed one |
| `-o` | `json` or `yaml` |

### `kona kernel install`

Download the guest kernel this kona pins from its GitHub release, check
its sha256, and install it.

| Flag | |
|---|---|
| `--from FILE` | install a local build (`kernel/out/vmlinux-<ver>-kona`) instead |

### `kona support bundle [NAME]`

Write a `.tar.gz` with the cluster's state, checks, node logs and Kubernetes
view, with secrets redacted. See
[Support bundle](../guide/troubleshooting.md#support-bundle).

| Flag | Default |
|---|---|
| `-o` | `kona-<name>-support-<time>.tar.gz` |

### `kona version`

Print kona's version and the pinned component versions.

### `kona completion SHELL`

Print a shell completion script (`bash`, `zsh`, `fish`, `powershell`).

## Files

| Path | |
|---|---|
| `~/Library/Application Support/kona/<cluster>/` | cluster state and secrets (override with `KONA_HOME`) |
| `~/Library/Application Support/kona/kernels/` | the guest kernel |
| `~/.kube/kona-<cluster>` | the cluster's kubeconfig |
