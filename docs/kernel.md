# kona guest kernel (Apple `container` nodes)

## What and why

Apple `container`'s default kernel (on this host: 6.18.35 from Kata 3.32.0,
`vmlinux-6.18.35-197-debug`, the default since container 1.3.0) already has
almost everything Kubernetes needs: cgroup v2, overlayfs, br_netfilter,
nf_tables with iptables compat, IPVS, VXLAN, BPF syscall/JIT, cgroup-BPF, and
BTF. What it lacks is the **tc eBPF datapath** (Cilium cannot attach at all)
plus a few link types.

kona therefore does **not** maintain its own kernel config. It uses
`kernel/base-arm64.config`, the exact running config extracted from
`/proc/config.gz` of the default kernel, and merges a small fragment,
[`kernel/kona.config`](../kernel/kona.config), onto it.

- Source: kernel.org `linux-6.18.35.tar.xz`, sha256-verified against
  `sha256sums.asc`. The version is pinned in `versions.env` (`KERNEL_VERSION`).
- Toolchain: `ubuntu:22.04` pinned by digest (gcc 11.4, pahole 1.25). This
  matches the base config's `CONFIG_CC_VERSION_TEXT`/`CONFIG_PAHOLE_VERSION`.
- The build runs inside an Apple `container` VM (`make -C kernel`), with no
  Docker.
- `build.sh` fails if any fragment option is dropped by `olddefconfig` (an
  unmet dependency) and prints the `CONFIG_*` name.
- `LOCALVERSION=-kona`, so `uname -r` = `6.18.35-kona`.
- The built kernel's sha256 is pinned in `versions.env`
  (`KONA_KERNEL_SHA256_ARM64`). `kona doctor` fails when the installed
  kernel doesn't match, so a stale kernel from an older kona that lacks newer
  options is never used silently.
- Output: `kernel/out/vmlinux-6.18.35-kona` (uncompressed arm64 `Image`,
  ~30 MB) plus `.sha256` and the effective `.config`. It is released as
  `kona-kernel-6.18.35-kona-arm64.tar.zst`.

## Added options (effective diff, base → kona)

Every line below is a real difference between `base-arm64.config` and the
built `.config`. Toolchain-version lines are excluded.

| Option | Base | kona | Why |
|--------|------|------|-----|
| `NET_CLS_ACT` | n | y | tc actions framework. Required by Cilium (bpf programs return TC_ACT_*). |
| `NET_CLS_BPF` | n | y | `cls_bpf` classifier. Cilium's tc datapath. |
| `NET_ACT_BPF` | – | y | `act_bpf`. Listed in Cilium's system requirements. |
| `NET_SCH_INGRESS` | – | y | Provides the `ingress` and **`clsact`** qdiscs where Cilium attaches. The spec calls this out explicitly. |
| `NETKIT` | n | y | netkit pod devices (Cilium ≥1.16 `bpf.datapathMode=netkit`). Optional, but it avoids a veth fallback. |
| `NETFILTER_XT_MATCH_SOCKET` | n | y | `xt_socket`. Cilium L7 proxy / TPROXY with kube-proxy replacement. |
| `CRYPTO_SHA1` | n | y | Listed in Cilium requirements (BPF program tag hashing, IPsec). |
| `SCHEDSTATS` | n | y | Listed in Cilium requirements (Hubble / process stats). |
| `INET_DIAG`, `INET_TCP_DIAG`, `INET_UDP_DIAG` | n / – / – | y | `sock_diag` netlink. Cilium kube-proxy replacement iterates sockets through it. Added in Phase 2, after `cilium connectivity test` failed its log check with `failed while iterating sockets: no such file or directory`. Also makes `ss` work in nodes. |
| `INET_DIAG_DESTROY` | – | y | `SOCK_DESTROY`: lets Cilium reset sockets still connected to a deleted Service backend (for example a UDP DNS client pinned to a removed CoreDNS pod). Added in Phase 2 with the options above. |
| `GENEVE` | n | y | Cilium `tunnelProtocol=geneve`. The spec requires VXLAN/Geneve; VXLAN was already `y`. |
| `WIREGUARD` | n | y | Networking option (c) overlay and Cilium transparent encryption. |
| `DUMMY` | n | y | kube-proxy IPVS mode creates the `kube-ipvs0` dummy interface. |
| `CRYPTO_LIB_CHACHA*`, `CRYPTO_LIB_CHACHA20POLY1305`, `CRYPTO_LIB_CURVE25519*`, `CRYPTO_LIB_POLY1305*` | – | y | Selected automatically by `WIREGUARD`. |
| `NET_ACT_*` (gact, mirred, police, …), `NET_TC_SKB_EXT`, `WIREGUARD_DEBUG` | – | n | New symbols exposed by `NET_CLS_ACT`/`WIREGUARD`, left at their default (off). |

Options already present in the default kernel, verified by
`spikes/01-kernel/check-config.sh`: `BRIDGE_NETFILTER`, `OVERLAY_FS`,
`NF_TABLES`, `NFT_COMPAT`, `IP_NF_*`, `IP_VS` (+rr/wrr/sh/nfct),
`BPF_SYSCALL`, `BPF_JIT`, `CGROUP_BPF`, `BPF_EVENTS`, `DEBUG_INFO_BTF`,
`VXLAN`, `VETH`, `BRIDGE`, `IP_SET`, `NETFILTER_XT_TARGET_TPROXY`, and the full
cgroup v2 controller set (`cpuset cpu io memory hugetlb pids`).

Deliberately **not** changed:

- `MODULES` stays `n`. The base kernel is monolithic, so `lsmod` is empty
  (there's no `/proc/modules`) and features are verified by probing, not by
  module lists.
- `BPF_JIT_ALWAYS_ON` stays unset. It's hardening, not a requirement.
- `DRM`/`DRM_VIRTIO_GPU` stay off. GPU nodes run under krunkit with the
  Fedora kernel, not this one.

## Verification

`spikes/01-kernel/probe.sh [kernel]` boots one VM and probes. The output for
both kernels is committed (`probe-default.txt`, `probe-kona.txt`). The diff is
exactly: tc BPF options now set, geneve/wireguard/dummy/netkit links can be
created, and the `clsact` qdisc attaches. See [gates.md](gates.md).

`kona doctor` and node boot use the same probe list to **fail fast with the
`CONFIG_*` name** when a user-supplied kernel is missing a feature.

## Installation policy

- **Default: per-container.** kona passes `container run --kernel
  ~/Library/Application Support/kona/kernels/vmlinux-<ver>-kona` for every
  node. The system-wide default kernel is never touched, so other `container`
  workloads are unaffected.
- **Opt-in: system-wide** (`kona kernel install --system`, with an interactive
  confirmation or `--yes`):
  1. kona records the current default from `container system property list`
     (`[kernel] binaryPath/url/digest`) and the target of the
     `kernels/default.kernel-arm64` symlink into
     `~/Library/Application Support/kona/kernel-previous.json`.
  2. It runs `container system kernel set --binary <kona kernel> --force`.
  3. `kona kernel restore` re-applies the recorded default (`--tar <url>
     --digest <digest> --binary <binaryPath>`, or `--recommended` if the
     previous default was the recommended one).
