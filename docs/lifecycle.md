# Cluster lifecycle: stop, start, verify

| Command | What it does |
|---|---|
| `kona stop cluster [NAME]` | Stops every node and keeps VMs, disks and state. Order: GPU nodes and their WireGuard relays, then workers, then control planes, so agents never outlive the API. `container` nodes get `container stop --time 30`, so init shuts k3s/kubelet down cleanly. |
| `kona start cluster [NAME] [--wait 5m]` | Boots stopped nodes, control planes first, restarts the relays, then waits until `kona verify cluster` passes. |
| `kona stop node NODE... [--name NAME]` | Stops just those nodes. The relays keep running, since they only forward to their node. |
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
new boot, and it needs no clock comparison (a freshly booted GPU VM's clock
may be off).

A fresh Ready still comes before Cilium, CoreDNS and the DRA plugin are
back. `start` then polls `verify` until nothing fails. Measured on the
`kona` cluster (k3s, Cilium, one GPU node): nodes were fresh-Ready about
21 s after start, and verify passed about 45 s later. `start` took 69 s in
total.

## Checks

| Check | Fails when |
|---|---|
| `vms` | A node's VM (`container` or krunkit) isn't running. |
| `relays` | A CPU node's WireGuard relay process is gone (GPU clusters with `--gpu-link relay`; otherwise skipped). |
| `host-api` | The host can't open a TCP connection to `<control plane>:6443`. |
| `api` | `/readyz` fails inside the control plane. The Kubernetes checks below are then skipped. |
| `nodes` | A node is unregistered or not Ready; is Ready but its VM is down (stale status); has an InternalIP other than its kona IP; or has a boot ID different from its VM's (Ready from an earlier boot). |
| `pods` | A kube-system or kona-system pod is not Running/Succeeded, is Running but not Ready, or waits in CrashLoopBackOff / image-pull / config errors. |
| `daemonsets` | Any DaemonSet (CNI, kube-proxy, kona-venus-dra) has fewer ready pods than scheduled. |
| `dns` | `kube-system/coredns` has no ready replica. |
| `clock` | Warns (doesn't fail) when a guest clock is more than 1 s off the host. |
| `gpu` | A Venus GPU node has no ResourceSlice, or a GPU node has no `kona.dev/gpu` label. Warns when a GPU node joined without Venus. |

With the GPU VM stopped and its Node still showing Ready:

```
$ kona stop node kona-gpu-worker-1
$ kona verify cluster
STATUS   CHECK        DETAIL
FAIL     vms          not running: kona-gpu-worker-1
PASS     relays       2 WireGuard relays running
PASS     host-api     192.168.70.10:6443 reachable from the host
PASS     api          readyz ok
FAIL     nodes        kona-gpu-worker-1 shows Ready but its VM is down (stale status)
PASS     pods         kube-system and kona-system pods healthy
PASS     daemonsets   ready: cilium, cilium-envoy, kona-venus-dra
PASS     dns          coredns 1 ready
PASS     clock        worst skew 0.03s
PASS     gpu          1 GPU node(s) publish a Venus ResourceSlice
! vms: kona start cluster kona
! nodes: see `kubectl describe node <node>`
error: 2 check(s) failed
```

A full stop and start:

```
$ kona stop cluster kona
• Stopping GPU node kona-gpu-worker-1
• Stopped 2 WireGuard relay(s)
• Stopping kona-worker-1
• Stopping kona-control-plane-1
✓ Stopped 3 node(s) of "kona"; resume with: kona start cluster kona
$ kona get clusters
NAME   PHASE   STATUS    DISTRO   NODES   SUBNET            ISSUES
kona   ready   stopped   k3s      0/3     192.168.70.0/24   -
$ kona start cluster kona
• Starting kona-control-plane-1
• Starting kona-worker-1
• Starting GPU node kona-gpu-worker-1
• Waiting up to 4m51s for 3 nodes to be Ready
✓ All 3 nodes Ready
• Waiting for system pods, DNS and GPUs
✓ Cluster "kona" is healthy
```

Not done here: `stop node` doesn't cordon or drain. Stopping a node is like
pulling its power, and pods on it are rescheduled by the usual node-lifecycle
timeouts.
