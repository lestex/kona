# Failure modes

Every entry here was hit in a real run (see [gates.md](gates.md)) or is
required by the brief. Each lists how it shows up and what kona does about it.

## Host and tooling

| Failure | Symptom | Handling |
|---|---|---|
| macOS < 26 | vmnet networks can't reach each other | `kona doctor` fails with the reason. kona never works around it silently. |
| Stale or wrong guest kernel | Missing features; for example Cilium can't iterate sockets | The kernel sha256 is pinned in `versions.env`. `kona doctor` and `create` fail and print the reinstall command. Node boot fails fast (exit 78) naming the missing `CONFIG_*`. |
| vmnet gateway DNS forwarder refuses queries | `Temporary failure resolving …` in nodes and builds | kona passes the host's first IPv4 resolver explicitly (`--dns`) and never relies on 192.168.x.1:53. |
| The host's first resolver is IPv6 (seen 2026-11-08: `nameserver[0] : 2600:1700:…`, IPv4 `192.168.1.254` second) | Nodes resolve names (vmnet gives them IPv6), but pods don't: CoreDNS logs `dial udp [2600:…]:53: connect: network is unreachable` and external names return SERVFAIL | kona uses the host's first IPv4 resolver from its default (not per-domain) configuration. `create` rejects an IPv6 or missing `--dns`, because the pod network is IPv4-only; `kona doctor` warns when the host has no IPv4 resolver. |
| Stale negative DNS cache on macOS (`github.com` unresolvable, public resolvers fine) | `curl: (6) Could not resolve host` for host-side downloads | Tool downloads fail loudly with the URL. Checksum pinning (`hack/pin-checksums.sh`) aborts on any failed lookup instead of writing empty pins. Fix: `sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder`, or run the script in a container. |
| User's Helm setup (repos, caches) | `cilium install` failed: `no cached repo found … az-index.yaml` | The Cilium CLI runs with `HELM_*` pointed at `~/Library/Application Support/kona/helm/`. kona never reads or changes the user's Helm config. |
| `container` upgraded (e.g. 1.4.1 → 1.5.0 → 1.7.1 by `brew upgrade`) | Its services restart and **stop every VM**. Each new build also gets its own macOS **Local Network** rule. On Homebrew's ad-hoc-signed 1.5.0 that rule ended up denied and TCP published ports failed (`connect failed … No route to host`, apple/container#2067). On 1.7.1 (macOS 27.2, 2026-11-08) TCP and UDP published ports work with the rule allowed **and** with it denied | `kona get clusters` shows `degraded` or `stopped`; run `kona start cluster`. kona reaches nodes at their IPs and publishes no ports, so the Local Network rule doesn't affect it. The version kona is tested with is pinned in `versions.env`. |
| A base image cached by an older `container` (seen after 1.5.0 → 1.7.1, 2026-11-08) | `make -C images/node build` fails at the first `RUN` with `failed to compute cache key: … strconv.ParseInt: parsing "": invalid syntax`. Only the digest-pinned base (`fedora:44@sha256:…`, listed with tag `<none>`) fails; `FROM fedora:44` and other images build | Not handled by kona: it's in `container`'s builder. Remove the stale copy so the builder pulls it again: `container image rm docker.io/library/fedora@<digest>` (the digest is `FEDORA_DIGEST` in `versions.env`). |
| Host sleep | `container` VMs survived an overnight idle-sleep cycle | Nothing needed; guest clocks are stepped by chrony (see below). |

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
| Host reboot, or `container` services restart | All `container` VMs stop | `kona start cluster` restarts control planes first, then workers, and waits until `kona verify cluster` passes ([lifecycle.md](lifecycle.md)). The entrypoint reapplies each node's static IP. |

## Lifecycle

| Failure | Handling |
|---|---|
| Partial `create` failure | Rolls back by default: VMs, volumes, network, kubeconfig and state are removed (verified: no orphans). `--retain` keeps everything flagged `failed`, and re-running `create` resumes it. |
| Orphans (resources labeled `kona.cluster` with no state) | `kona get clusters` flags them. `kona delete cluster <name>` and `--all` delete from labels, so this works without state. |
| Wi-Fi change | Node↔node and host↔node paths are unaffected ([networking.md](networking.md)). |
| Cluster created by an older kona with GPU nodes | `start`, `stop`, `add node` and `verify` refuse it and point at the `gpu` branch. `kona delete cluster` removes its `container` parts and warns about the krunkit VM. |

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

The forwarding veth has MTU 1500, so the overlay MTU is not the cause. `FragFails` is the counter `ip_forward` bumps for these drops.

**Options considered.** None are applied:

- An edge proxy that terminates the connection on the node, where local
  delivery works, and opens a new one to the pod. This is what kiac does
  (its issue #8). It is a workaround in the data path.
- Turning off TSO on the client VM fixes it, but only for VMs you control.
