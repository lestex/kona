# Failure modes

Every entry here was hit in a real run (see [gates.md](gates.md)) or is
required by the brief. Each lists how it shows up and what kona does about it.

## Host and tooling

| Failure | Symptom | Handling |
|---|---|---|
| macOS < 26 | vmnet networks can't reach each other, and vmnet-helper needs root | `kona doctor` fails with the reason. kona never works around it silently. |
| Stale or wrong guest kernel | Missing features; for example Cilium can't iterate sockets | The kernel sha256 is pinned in `versions.env`. `kona doctor` and `create` fail and print the reinstall command. Node boot fails fast (exit 78) naming the missing `CONFIG_*`. |
| vmnet gateway DNS forwarder refuses queries | `Temporary failure resolving …` in nodes and builds | kona passes the host's primary resolver explicitly (`--dns`) and never relies on 192.168.x.1:53. |
| Stale negative DNS cache on macOS (`github.com` unresolvable, public resolvers fine) | `curl: (6) Could not resolve host` for host-side downloads | Tool downloads fail loudly with the URL. Checksum pinning (`hack/pin-checksums.sh`) aborts on any failed lookup instead of writing empty pins. Fix: `sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder`, or run the script in a container. |
| User's Helm setup (repos, caches) | `cilium install` failed: `no cached repo found … az-index.yaml` | The Cilium CLI runs with `HELM_*` pointed at `~/Library/Application Support/kona/helm/`. kona never reads or changes the user's Helm config. |
| `container` upgraded (e.g. 1.4.1 → 1.5.0 by `brew upgrade`) | Its services restart and **stop every VM**. Afterwards `--publish` fails with `connect failed … No route to host` (EHOSTUNREACH) while the container IP works directly (apple/container#2067). The new binaries need the macOS **Local Network** privacy permission again, and kona's GPU↔CPU WireGuard link depends on published UDP ports | `kona get clusters` shows `degraded` and suggests `kona start cluster`. Approve the Local Network prompt (System Settings → Privacy & Security → Local Network → enable `container`), then run `kona start cluster`. The version kona is tested with is pinned in `versions.env`. |
| Host sleep | `container` VMs survived an overnight idle-sleep cycle, but the krunkit GPU VM and its vmnet-helper did not; both were gone with no log line and no crash report. The cause is not yet known | `kona start cluster` boots it again from its disk. Its configuration (static IP, WireGuard, k3s) is on the disk, so it rejoins. |
| deprecated `slp/krunkit` tap | krunkit stuck on 1.1.1 | `kona doctor` detects it and prints the migration commands to `libkrun/krun`. |

## Node VMs (Apple `container`)

| Failure | Symptom | Handling |
|---|---|---|
| `container` gives a new IP on every run, even with a pinned MAC | Node IPs change on recreate | The entrypoint makes kona's static IP the primary `eth0` address. The dynamic one stays as a secondary. |
| cgroup v2 "no internal processes" | kubelet: `cannot enter cgroupv2 "/sys/fs/cgroup/kubepods" … invalid state` | The k3s entrypoint moves all processes into `/init` (snapshotting `cgroup.procs` each round) and delegates controllers. systemd nodes manage the root cgroup themselves. |
| Private mount propagation (all mounts start `private`; systemd in a container doesn't `make-rshared`) | Cilium: `path "/sys/fs/bpf" is mounted on "/sys" but it is not a shared mount` | The entrypoint runs `mount --make-rshared /`, mounts bpffs on `/sys/fs/bpf`, and makes it shared, for both distros. |
| Fresh ext4 volume has `lost+found` | kubeadm preflight: `/var/lib/etcd is not empty` | The etcd `dataDir` is `/var/lib/etcd/data`, a subdirectory of the block volume. |
| Default container MTU 1280 | Fragmentation or drops for overlay traffic | Nodes run with `mtu=1500`. |
| vmnet passes TSO super-frames to a VM unsegmented ([details](#vmnet-tso-super-frames)) | Uploads of more than ~256 KiB from another `container` VM to a NodePort stall until timeout. Downloads, and uploads from the macOS host or from pods, work | Not handled. It is a vmnet/`container` bug, and kona adds no proxy around it. Reproduce with `hack/repro-vmnet-tso.sh`. |
| Guest clock drift after host sleep | Skewed timestamps and TLS/token errors | chrony runs with `makestep 1 -1`. `kona get nodes` shows skew per node: after a real overnight sleep/wake cycle, skew was +0.06 s to +0.20 s. |
| Host reboot, or `container` services restart | All `container` VMs stop, and krunkit VMs are gone | `kona start cluster` restarts control planes first, then workers and GPU nodes, and waits for Ready. Static IPs and WireGuard configs are reapplied from the node disks. |

## Lifecycle

| Failure | Handling |
|---|---|
| Partial `create` failure | Rolls back by default: VMs, volumes, network, kubeconfig and state are removed (verified: no orphans). `--retain` keeps everything flagged `failed`, and re-running `create` resumes it. |
| Orphans (resources labeled `kona.cluster` with no state) | `kona get clusters` flags them. `kona delete cluster <name>` and `--all` delete from labels, so this works without state. |
| vmnet-helper hangs in `vmnet_start_interface` (colliding subnet or reused interface ID); SIGTERM is ignored | 10 s start timeout, then SIGKILL. A fresh interface ID is used on every start (Phase 3). |
| `container` can't join krunkit VMs to its network, and separate vmnet networks are isolated | GPU nodes are reached over WireGuard via a published UDP port ([networking.md](networking.md)). |
| Wi-Fi change | L2 paths are unaffected. The GPU↔CPU tunnel can stall for up to ~40 s until keepalive traffic restores it. |
| Venus unavailable (Phase 3) | The GPU node joins untainted without the DRA plugin, with a loud warning. |

## vmnet TSO super-frames

**What happens.** A `container` VM sends with TSO on, which is the default.
vmnet hands the resulting super-frames, several MSS long, to the receiving VM
without splitting them. The receiving kernel accepts them for local sockets,
because local delivery doesn't check the MTU. If the receiver has to
*forward* them (NodePort or LoadBalancer traffic to a pod, with either
kube-proxy or Cilium KPR), `ip_forward` drops each one with `PKT_TOO_BIG`.
The sender's MSS already fits, so the ICMP "fragmentation needed" reply
changes nothing. The sender keeps retransmitting and the connection stalls.

**Who is affected.** Only senders that are Apple `container` VMs on a vmnet
network. TCP from the macOS host segments correctly. Traffic between nodes
inside the cluster and traffic from pods are fine. Downloads are fine too,
because the node's side of the stream is segmented before vmnet sees it.

**Evidence** (cluster `kona`, k3s and Cilium 1.20.2 KPR, server pod on
`kona-worker-1`, client a plain `alpine` container on `kona-kona`):

| Test | Result |
|---|---|
| GET 64 MiB via NodePort (pod's node, and the other node) | 10/10 ok, sha256 match |
| POST ≤ 256 KiB via NodePort | ok |
| POST ≥ 1 MiB via NodePort, client MTU 1280 or 1500 | curl rc=28 (timeout) every time |
| same POST, `ethtool -K eth0 tso off gso off` on the client | ok |
| POST 8 MiB to a hostNetwork listener on the node (no forwarding) | ok |
| POST 64 MiB from a pod to the Service's ClusterIP | ok |
| POST 8 MiB from the macOS host via NodePort | ok |

What the node saw during a stalled upload (MSS 1240):

- On `eth0`, frames of 1228, 2456, 3684, 4912, 6140 and 8596 bytes arrive.
  They still arrive with GRO turned off on the node, so vmnet delivers them
  that way.
- On the pod's veth, only 1228-byte single segments appear. Cilium logs no
  drop.
- The `skb:kfree_skb` tracepoint shows
  `153 location=ip_forward+0x300/0x5f8 reason: PKT_TOO_BIG`.

**Repro without Kubernetes.** `hack/repro-vmnet-tso.sh [network] [dns]`
starts a router container that forwards to a server in a veth netns
(MTU 1500). It then uploads 8 MiB from a client container, with TSO on and
off, to the router itself and through it. On Apple's default kernel, on the
`default` network:

```
router 192.168.64.13 on default, server 10.99.0.2 behind it (veth mtu 1500)
TSO    path                               received FragFails+
on     client -> router (local)         8388608/8388608 0
on     client -> router -> netns (fwd)  310280/8388608 244
off    client -> router (local)         8388608/8388608 0
off    client -> router -> netns (fwd)  8388608/8388608 0
```

The forwarding veth has MTU 1500, so kona's Cilium MTU (1420) is not the
cause. `FragFails` is the counter `ip_forward` bumps for these drops.

**Options considered.** None are applied:

- An edge proxy that terminates the connection on the node, where local
  delivery works, and opens a new one to the pod. This is what kiac does
  (its issue #8). It is a workaround in the data path.
- Turning off TSO on the client VM fixes it, but only for VMs you control.
