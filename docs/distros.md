# Distros: k3s (default) vs kubeadm

`kona create cluster --distro k3s|kubeadm`. Both run on the same kernel, the
same networking, and the same kona node lifecycle. They differ in what runs
inside the node VM.

| | k3s (default) | kubeadm (opt-in) |
|---|---|---|
| Node image | Fedora 44 + one `k3s` binary (containerd, runc, flannel, CNI plugins, kubectl bundled) + air-gap system images | Fedora 44 + systemd + containerd + kubelet/kubeadm/kubectl |
| PID 1 in the node | `container`'s init (`--init`), then `kona-node` → `k3s` | systemd |
| Create time, 1 CP + 2 workers | **12.7 s** (measured, [gates.md](gates.md)) | not measured yet |
| Datastore | single server: SQLite (kine) on the control plane's block volume. HA (`--control-planes 3`): embedded etcd on that volume | etcd static pod, data dir on the block volume; `etcdctl check perf` in the gate |
| Bundled add-ons | CoreDNS, local-path-provisioner, metrics-server, ServiceLB (traefik disabled by kona) | CoreDNS and kube-proxy only; kona installs the CNI |
| Fidelity to upstream | Upstream Kubernetes, packaged differently (k3s flags, kine, single process) | Closest to how production clusters are built (static pods, kubeadm config, separate components) |
| Memory at idle, per node | lower: one process | higher: separate apiserver, controller-manager, scheduler, and etcd on the CP |

**Why k3s is the default:** boot time and footprint. Every node is a VM with
fixed memory (no ballooning), so a lighter node lets you run more of them on a
laptop. `kona create cluster` should feel as fast as `kind create cluster`,
and k3s's air-gap images mean nodes start without pulling anything.

**When to use kubeadm:** you need upstream component layout (static pods,
kubeadm-managed certs and upgrades, a separate etcd), you're testing
something that behaves differently under k3s (kine vs etcd, bundled
add-ons), or you want `etcdctl check perf` numbers for the datastore.
