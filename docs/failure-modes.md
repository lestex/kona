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
