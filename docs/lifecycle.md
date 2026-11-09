# Cluster lifecycle: stop, start, verify

| Command | What it does |
|---|---|
| `kona stop cluster [NAME]` | Stops every node and keeps VMs, disks and state. Order: workers, then control planes, so agents never outlive the API. `container` nodes get `container stop --time 30`, so init shuts k3s/kubelet down cleanly. |
| `kona start cluster [NAME] [--wait 5m]` | Boots stopped nodes, control planes first, then waits until `kona verify cluster` passes. |
| `kona stop node NODE... [--name NAME]` | Stops just those nodes. Refuses to stop control planes if fewer than etcd quorum (a majority, so the only one in a single-control-plane cluster) would stay running, since the API would go down; use `kona stop cluster` for that. |
| `kona start node NODE... [--name NAME] [--wait 5m]` | Boots just those nodes and waits for them, and for the cluster, to be healthy. |
| `kona verify cluster [NAME] [-o json\|yaml]` | Read-only health checks. Exits 1 if any check fails. |

`kona get clusters` shows `stopped` for a cluster whose nodes all exist and
none are running, and `degraded` when only some are down.

## What "started" means

A Node object keeps `Ready=True` from before its VM stopped until the node
controller notices, about 40 s later. `start` used to return as soon as it
saw that stale Ready. Now it reads each booted VM's
`/proc/sys/kernel/random/boot_id` and waits until the Node's
`status.nodeInfo.bootID` matches. That is the kubelet reporting from the
new boot, and it needs no clock comparison (a freshly booted VM's clock may
be off).

A fresh Ready still comes before Cilium and CoreDNS are back. `start` then
polls `verify` until nothing fails, and so does `create cluster` (without
it, `create` returned about 20 s before CoreDNS was ready). Measured on the `kona` cluster (k3s,
Cilium, then still with a GPU node): nodes were fresh-Ready about 21 s after
start, and verify passed about 45 s later. `start` took 69 s in total.

## Checks

| Check | Fails when |
|---|---|
| `vms` | A node's VM isn't running. |
| `host-api` | The host can't open a TCP connection to `<control plane>:6443`. |
| `api` | `/readyz` fails inside the control plane. The Kubernetes checks below are then skipped. |
| `nodes` | A node is unregistered or not Ready; is Ready but its VM is down (stale status); has an InternalIP other than its kona IP; or has a boot ID different from its VM's (Ready from an earlier boot). |
| `pods` | A kube-system or kona-system pod is not Running/Succeeded, is Running but not Ready, or waits in CrashLoopBackOff / image-pull / config errors. |
| `daemonsets` | Any DaemonSet (CNI, kube-proxy) has fewer ready pods than scheduled. |
| `dns` | `kube-system/coredns` has no ready replica. |
| `clock` | Warns (doesn't fail) when a guest clock is more than 1 s off the host. |

`kona verify cluster` prints one line per check (`PASS`, `WARN`, `FAIL` or
`SKIP`) with a hint for each failure; `-o json` gives the same report as
data. Clusters made by an older kona with GPU nodes are refused by `start`,
`stop`, `add node` and `verify`; `kona delete cluster` still removes them.

Not done here: `stop node` doesn't cordon or drain. Stopping a node is like
pulling its power, and pods on it are rescheduled by the usual node-lifecycle
timeouts.
