---
title: "Troubleshooting"
linkTitle: "Troubleshooting"
weight: 5
---
## Start with the checks

```sh
kona doctor             # the Mac: macOS, arch, container, kernel, DNS
kona verify cluster     # a cluster: VMs, API, nodes, pods, DNS, clocks
kona get nodes          # per node: VM state, Ready, clock skew
```

Each failed check says what is wrong and, where kona knows, how to fix it.
`verify` only reads; it never changes the cluster. All three take
`-o json`.

## Common problems

**`create` fails with "guest kernel … not found" or "not the build this
kona expects".** Run `kona kernel install`. After upgrading kona, the
pinned kernel can change too. Existing clusters keep the kernel they were
created with; new clusters and new nodes get the new one.

**A cluster is `stopped` after the Mac rebooted.** That's expected: VMs
don't start by themselves. Run `kona start cluster`.

**`create` fails pulling the node image.** Check that the Mac can reach
`ghcr.io`. Behind a proxy or offline, build the image locally from
`images/node` and pass it with `--image`.

**Pods can't resolve names.** Check `kona doctor`'s `dns` line: nodes and
CoreDNS use the IPv4 resolver it shows. If that resolver only answers on
some networks (a VPN's, say), create the cluster with
`--dns <a resolver that always works>`.

**`kona get clusters` shows `orphan`.** `container` has VMs, volumes or a
network labeled with a kona cluster that kona has no state for, for
example after the state folder was deleted. `kona delete cluster --all`
removes them.

**`container` itself misbehaves after an upgrade.** Restart its services
with `container system stop && container system start`, then
`kona start cluster`.

[Failure modes](../reference/failure-modes.md) lists every problem found while
building kona, with its root cause.

## A shell on a node

```sh
kona ssh kona-worker-1
kona ssh kona-control-plane-1 -- crictl ps
```

It's a `container exec`, so it works even when the node's network doesn't.
k3s nodes log to the VM's console (`container logs kona-worker-1`).
kubeadm nodes run systemd, so use `journalctl -u kubelet` inside the node.

## Support bundle

```sh
kona support bundle [NAME]
```

This writes `kona-<name>-support-<time>.tar.gz` with everything needed to
debug a cluster from somewhere else:

- kona's state, `doctor` and `verify` reports, and `container`'s view;
- per node: logs, network config, `dmesg`, processes and disk usage;
- from Kubernetes: nodes, pods, DaemonSets and events.

Join tokens and kubeconfigs are never included, and secret values are
redacted wherever they appear in logs. Logs can still contain hostnames and
IP addresses, so look through the bundle before sharing it. Attach it to a
[GitHub issue](https://github.com/lestex/kona/issues).
