# Gates

Every block below is verbatim output from a real run. Sources are the files under `spikes/`, which the scripts next to them regenerate.

## Phase 0: feasibility spikes

Host: macOS 26.6.2 (25G83), arm64, Apple M3 Pro. Runs: 2026-09-25/26.

### 0.1 Custom kernel on Apple `container`: PASS

`spikes/01-kernel/probe.sh kernel/out/vmlinux-6.18.35-kona` (build: `make -C kernel`, sha256 `774248597211c45650d02ef886f653524293970cd4fd9877550e0f2d79619493`)

```text
## uname -r
6.18.35-kona
## lsmod
lsmod: /proc/modules: No such file or directory
Module                  Size  Used by    Not tainted
(no /proc/modules => CONFIG_MODULES=n, everything built in)
## cgroup2
none on /sys/fs/cgroup type cgroup2 (rw,nosuid,nodev,noexec,relatime)
cpuset cpu io memory hugetlb pids
## bpftool feature probe kernel (filtered)
JIT compiler is enabled
CONFIG_BPF_SYSCALL is set to y
CONFIG_BPF_JIT is set to y
CONFIG_DEBUG_INFO_BTF is set to y
CONFIG_CGROUP_BPF is set to y
CONFIG_BPF_EVENTS is set to y
CONFIG_NET_ACT_BPF is set to y
CONFIG_NET_CLS_BPF is set to y
CONFIG_NET_CLS_ACT is set to y
CONFIG_NET_SCH_INGRESS is set to y
eBPF program_type sched_cls is available
eBPF program_type sched_act is available
eBPF program_type xdp is available
eBPF program_type cgroup_skb is available
eBPF program_type cgroup_sock_addr is available
eBPF map_type lru_hash is available
eBPF map_type lpm_trie is available
eBPF map_type hash_of_maps is available
eBPF map_type ringbuf is available
## link types
vxlan ok
geneve ok
wireguard ok
dummy ok
netkit ok
## tc clsact
qdisc clsact ffff: parent ffff:fff1 
## br_netfilter
bridge-nf-call-arptables
bridge-nf-call-ip6tables
bridge-nf-call-iptables
## ip_vs
IP Virtual Server version 1.2.1 (size=4096)
## nf_tables
table inet kt
## eth0 mtu
1280
```

Same probe on the default kernel, as a diff (`diff probe-default.txt probe-kona.txt`):

```diff
2c2
< 6.18.35
---
> 6.18.35-kona
17,20c17,20
< CONFIG_NET_ACT_BPF is not set
< CONFIG_NET_CLS_BPF is not set
< CONFIG_NET_CLS_ACT is not set
< CONFIG_NET_SCH_INGRESS is not set
---
> CONFIG_NET_ACT_BPF is set to y
> CONFIG_NET_CLS_BPF is set to y
> CONFIG_NET_CLS_ACT is set to y
> CONFIG_NET_SCH_INGRESS is set to y
32,35c32,35
< geneve FAIL
< wireguard FAIL
< dummy FAIL
< netkit FAIL
---
> geneve ok
> wireguard ok
> dummy ok
> netkit ok
37c37
< Error: Cannot find ingress queue for specified device.
---
> qdisc clsact ffff: parent ffff:fff1 
```

### 0.2 krunkit + Venus: PASS (with the patched Mesa)

`spikes/02-krunkit/up.sh`, then in the guest:

```text
$ cat /etc/fedora-release; uname -r
Fedora release 44 (Forty Four)
6.19.10-300.fc44.aarch64
$ rpm -q mesa-vulkan-drivers
mesa-vulkan-drivers-25.3.6-102.fc44.aarch64
$ ls -l /dev/dri
total 0
drwxr-xr-x. 2 root root         80 Sep 26 13:33 by-path
crw-rw----. 1 root video  226,   0 Sep 26 13:33 card0
crw-rw-rw-. 1 root render 226, 128 Sep 26 13:33 renderD128
$ vulkaninfo --summary
WARNING: [Loader Message] Code 0 : terminator_CreateInstance: Received return code -9 from call to vkCreateInstance in ICD /usr/lib64/libvulkan_dzn.so. Skipping this driver.
'DISPLAY' environment variable not set... skipping surface info
MESA: error: Opening /dev/dri/card0 failed: Permission denied
could not connect vdrm
Error opening virtio-gpu device for Asahi native context
WARNING: [Loader Message] Code 0 : ICD for selected physical device does not export vkGetPhysicalDeviceDisplayPlanePropertiesKHR!
WARNING: [Loader Message] Code 0 : ICD for selected physical device does not export vkGetPhysicalDeviceDisplayPropertiesKHR!
==========
VULKANINFO
==========

Vulkan Instance Version: 1.4.341


Instance Extensions: count = 25
-------------------------------
VK_EXT_acquire_drm_display             : extension revision 1
VK_EXT_acquire_xlib_display            : extension revision 1
VK_EXT_debug_report                    : extension revision 10
VK_EXT_debug_utils                     : extension revision 2
VK_EXT_direct_mode_display             : extension revision 1
VK_EXT_display_surface_counter         : extension revision 1
VK_EXT_headless_surface                : extension revision 1
VK_EXT_layer_settings                  : extension revision 2
VK_EXT_surface_maintenance1            : extension revision 1
VK_EXT_swapchain_colorspace            : extension revision 5
VK_KHR_device_group_creation           : extension revision 1
VK_KHR_display                         : extension revision 23
VK_KHR_external_fence_capabilities     : extension revision 1
VK_KHR_external_memory_capabilities    : extension revision 1
VK_KHR_external_semaphore_capabilities : extension revision 1
VK_KHR_get_display_properties2         : extension revision 1
VK_KHR_get_physical_device_properties2 : extension revision 2
VK_KHR_get_surface_capabilities2       : extension revision 1
VK_KHR_portability_enumeration         : extension revision 1
VK_KHR_surface                         : extension revision 25
VK_KHR_surface_protected_capabilities  : extension revision 1
VK_KHR_wayland_surface                 : extension revision 6
VK_KHR_xcb_surface                     : extension revision 6
VK_KHR_xlib_surface                    : extension revision 6
VK_LUNARG_direct_driver_loading        : extension revision 1

Instance Layers: count = 1
--------------------------
VK_LAYER_MESA_device_select Linux device selection layer 1.4.303  version 1

Devices:
========
GPU0:
	apiVersion         = 1.2.0
	driverVersion      = 25.3.6
	vendorID           = 0x106b
	deviceID           = 0x1a060209
	deviceType         = PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU
	deviceName         = Virtio-GPU Venus (Apple M3 Pro)
	driverID           = DRIVER_ID_MESA_VENUS
	driverName         = venus
	driverInfo         = Mesa 25.3.6
	conformanceVersion = 1.4.0.0
	deviceUUID         = 292174f7-f58e-bcdb-5428-6d869abed553
	driverUUID         = 50f74952-8719-d465-f68a-8256dca047a7
GPU1:
	apiVersion         = 1.4.328
	driverVersion      = 25.3.6
	vendorID           = 0x10005
	deviceID           = 0x0000
	deviceType         = PHYSICAL_DEVICE_TYPE_CPU
	deviceName         = llvmpipe (LLVM 22.1.0, 128 bits)
	driverID           = DRIVER_ID_MESA_LLVMPIPE
	driverName         = llvmpipe
	driverInfo         = Mesa 25.3.6 (LLVM 22.1.0)
	conformanceVersion = 1.3.1.1
	deviceUUID         = 6d657361-3235-2e33-2e36-000000000000
	driverUUID         = 6c6c766d-7069-7065-5555-494400000000
$ dmesg | grep drm
[    2.338279] [drm] Host memory window: 0x180000000 +0x480000000
[    2.338286] [drm] features: +virgl +edid +resource_blob +host_visible
[    2.338287] [drm] features: +context_init
[    2.338370] [drm] KMS disabled
[    2.338372] [drm] number of cap sets: 5
[    2.338664] [drm] cap set 0: id 1, max-version 1, max-size 308
[    2.338686] [drm] cap set 1: id 2, max-version 2, max-size 1384
[    2.338706] [drm] cap set 2: id 4, max-version 0, max-size 156
```

Upstream Fedora Mesa (26.2.3 and 26.0.3) fails. Excerpt captured before the COPR Mesa was installed (only the build path is shortened):

```text
WARNING: [Loader Message] Code 0 : terminator_CreateInstance: Received return code -9 from call to vkCreateInstance in ICD /usr/lib64/libvulkan_dzn.so. Skipping this driver.
ERROR at .../vulkaninfo/./vulkaninfo.h:613:vkCreateInstance failed with ERROR_OUT_OF_HOST_MEMORY
[  495.842236] [drm:virtio_gpu_dequeue_ctrl_func [virtio_gpu]] *ERROR* response 0x1200 (command 0x208)
[  495.842646] [drm:virtio_gpu_dequeue_ctrl_func [virtio_gpu]] *ERROR* response 0x1200 (command 0x209)
```

### 0.3 Networking: option (c) PASS, (a) not possible, (b) blocked by default and pending a root-only check

Isolation evidence (`spikes/03-net/isolation.sh`):

```text
## host: forwarding + routes
net.inet.ip.forwarding: 1
192.168.64         link#32            UC              bridge100      !
192.168.65         link#35            UC              bridge101      !
192.168.66         link#37            UC              bridge102      !
## (a) vmnet-helper on the container network's subnet (192.168.66.0/24)
ERROR [main] vmnet_start_interface: (unknown status)
## (b) container (192.168.66.4, bridge102) -> krunkit (192.168.65.10, bridge101) via host
2 packets transmitted, 0 received, 100% packet loss, time 1031ms

## krunkit -> container
2 packets transmitted, 0 received, 100% packet loss, time 1021ms

## where the packets stop: tcpdump on both host bridges while the container pings
[bridge102 ingress] 09:57:25.379505 IP 192.168.66.4 > 192.168.65.10: ICMP echo request, id 40, seq 1, length 64
[bridge102 ingress] 09:57:26.411848 IP 192.168.66.4 > 192.168.65.10: ICMP echo request, id 40, seq 2, length 64
[bridge101 egress ] 
(nothing on bridge101 => not forwarded)
## two container networks are isolated too: buildkit (default net) -> 192.168.66.4
--- 192.168.66.4 ping statistics ---
2 packets transmitted, 0 packets received, 100% packet loss
```

WireGuard overlay, container underlay MTU 1280 (`spikes/03-net/measure.sh`):

```text
## underlay MTU
cpu1 eth0: 1280  gpu0 eth0: 1500  wg0: 1420
## source IP as seen by the receiver (tcpdump on wg0)
13:51:49.401554 IP 10.99.0.21 > 10.99.0.10: ICMP echo request, id 25, seq 0, length 64
13:51:51.477619 IP 10.99.0.10 > 10.99.0.21: ICMP echo request, id 59226, seq 1, length 64
## largest unfragmented ICMP payload over wg0 (DF set)
payload 1392 dropped
payload 1352 dropped
payload 1300 ok (packet 1328)
## iperf3 gpu0 -> cpu1 over wg (10s)
[  5]   0.00-10.00  sec   440 MBytes   369 Mbits/sec   29            sender
[  5]   0.00-10.01  sec   439 MBytes   368 Mbits/sec                  receiver

iperf Done.
## iperf3 cpu1 -> gpu0 over wg (10s, reverse)
[  5]   0.00-10.00  sec   435 MBytes   365 Mbits/sec    0            sender
[  5]   0.00-10.00  sec   434 MBytes   364 Mbits/sec                  receiver

iperf Done.
```

WireGuard overlay, container underlay `mtu=1500`:

```text
## underlay MTU
cpu1 eth0: 1500  gpu0 eth0: 1500  wg0: 1420
## source IP as seen by the receiver (tcpdump on wg0)
13:56:33.748937 IP 10.99.0.21 > 10.99.0.10: ICMP echo request, id 23, seq 1, length 64
13:56:35.859744 IP 10.99.0.10 > 10.99.0.21: ICMP echo request, id 59231, seq 1, length 64
## largest unfragmented ICMP payload over wg0 (DF set)
payload 1392 ok (packet 1420)
## iperf3 gpu0 -> cpu1 over wg (10s)
[  5]   0.00-10.00  sec   438 MBytes   367 Mbits/sec   30            sender
[  5]   0.00-10.02  sec   437 MBytes   366 Mbits/sec                  receiver

iperf Done.
## iperf3 cpu1 -> gpu0 over wg (10s, reverse)
[  5]   0.00-10.00  sec   435 MBytes   365 Mbits/sec    0            sender
[  5]   0.00-10.00  sec   434 MBytes   364 Mbits/sec                  receiver

iperf Done.
```

Wi-Fi change (`spikes/03-net/wifi-toggle.sh`), run 1:

```text
-- before (10:00:54)
bridge100 bridge101 bridge102 
host -> gpu0 192.168.65.10: ok
host -> cpu1 static 192.168.66.200: ok
gpu0 -> cpu1 over wg: ok
cpu1 -> gpu0 over wg: ok
cpu1 handshake: 
cpu1 -> internet: ok
== Wi-Fi off
-- Wi-Fi off (10:01:02)
bridge100 bridge101 bridge102 
host -> gpu0 192.168.65.10: ok
host -> cpu1 static 192.168.66.200: ok
gpu0 -> cpu1 over wg: ok
cpu1 -> gpu0 over wg: ok
cpu1 handshake: 
cpu1 -> internet: FAIL
== Wi-Fi on
-- Wi-Fi back (10:01:16)
bridge100 bridge101 bridge102 
host -> gpu0 192.168.65.10: ok
host -> cpu1 static 192.168.66.200: ok
gpu0 -> cpu1 over wg: FAIL
cpu1 -> gpu0 over wg: FAIL
cpu1 handshake: 
cpu1 -> internet: ok
# (run 1 used an earlier script revision: no recovery timer, and the
#  handshake field was empty because busybox awk lacks systime().)
# Manual recheck ~40s later:
after +40s: gpu0->cpu1 ok
cpu1 handshake age: 68s
```

Run 2 (with recovery timer):

```text
-- before (10:02:46)
bridge100 bridge101 bridge102 
host -> gpu0 192.168.65.10: ok
host -> cpu1 static 192.168.66.200: ok
gpu0 -> cpu1 over wg: ok
cpu1 -> gpu0 over wg: ok
cpu1 handshake: 83s ago
cpu1 -> internet: ok
== Wi-Fi off
-- Wi-Fi off (10:02:54)
bridge100 bridge101 bridge102 
host -> gpu0 192.168.65.10: ok
host -> cpu1 static 192.168.66.200: ok
gpu0 -> cpu1 over wg: ok
cpu1 -> gpu0 over wg: ok
cpu1 handshake: 91s ago
cpu1 -> internet: FAIL
== Wi-Fi on
-- Wi-Fi back (10:03:02)
bridge100 bridge101 bridge102 
host -> gpu0 192.168.65.10: ok
host -> cpu1 static 192.168.66.200: ok
gpu0 -> cpu1 over wg: ok
cpu1 -> gpu0 over wg: ok
cpu1 handshake: 100s ago
cpu1 -> internet: ok
== waiting for gpu0 -> cpu1 over wg to recover
recovered 3s after Wi-Fi came back
-- recovered (10:03:05)
bridge100 bridge101 bridge102 
host -> gpu0 192.168.65.10: ok
host -> cpu1 static 192.168.66.200: ok
gpu0 -> cpu1 over wg: ok
cpu1 -> gpu0 over wg: ok
cpu1 handshake: 102s ago
cpu1 -> internet: ok
```

Sleep/wake: **not run yet** (deferred by the user; see [networking.md](networking.md#open-items-need-user-action-before-phase-1)).


## Phase 1: `kona create cluster --workers 2` on Apple `container`

### 1.1 k3s path: PASS

Run on 2026-09-26/27. Node image `ghcr.io/lestex/kona-node:v1.34.11-k3s1` was built locally with `make -C images/node build`; it is not published yet.

```text
$ kona doctor
✓ macos            macOS 26.6.2
✓ arch             arm64
✓ container        container 1.4.1
✓ container-system services running
✓ kernel           /Users/lestex/Library/Application Support/kona/kernels/vmlinux-6.18.35-kona
✓ dns              nodes will use 8.8.8.8 (the vmnet gateway forwarder is not relied on)
✓ krunkit          krunkit 1.3.2
✓ vmnet-helper     vmnet-helper 0.13.0

$ time kona create cluster --workers 2
• Planned cluster "kona": 1 control plane(s), 2 worker(s) on 192.168.70.0/24
• Creating network kona-kona (192.168.70.0/24)
• Starting kona-control-plane-1 (192.168.70.10, 2 CPU, 2G)
• Waiting for the API server on 192.168.70.10
• Starting kona-worker-1 (192.168.70.20, 2 CPU, 2G)
• Starting kona-worker-2 (192.168.70.21, 2 CPU, 2G)
• Waiting up to 4m52s for 3 nodes to be Ready
✓ All 3 nodes Ready
✓ Wrote kubeconfig /Users/lestex/.kube/kona-kona (context kona-kona)
✓ Cluster "kona" is ready

  kubectl --kubeconfig /Users/lestex/.kube/kona-kona get nodes

./bin/kona create cluster --workers 2 2>&1  0.52s user 0.29s system 6% cpu 12.705 total

$ kubectl --kubeconfig ~/.kube/kona-kona get nodes -o wide      # from the macOS host
NAME                   STATUS   ROLES           AGE     VERSION         INTERNAL-IP     EXTERNAL-IP   OS-IMAGE                            KERNEL-VERSION   CONTAINER-RUNTIME
kona-control-plane-1   Ready    control-plane   2m13s   v1.34.11+k3s1   192.168.70.10   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona     containerd://2.2.7-k3s1
kona-worker-1          Ready    <none>          2m8s    v1.34.11+k3s1   192.168.70.20   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona     containerd://2.2.7-k3s1
kona-worker-2          Ready    <none>          2m7s    v1.34.11+k3s1   192.168.70.21   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona     containerd://2.2.7-k3s1

$ ls -l ~/.kube/kona-kona
-rw-------@ 1 lestex  staff  2961 Sep 26 21:07 /Users/lestex/.kube/kona-kona

$ kona get clusters
NAME   PHASE   STATUS   DISTRO   NODES   SUBNET            ISSUES
kona   ready   ok       k3s      3/3     192.168.70.0/24   -

$ kona get nodes
NAME                   ROLE            KIND        IP              VM        READY   VERSION         CLOCK SKEW
kona-control-plane-1   control-plane   container   192.168.70.10   running   true    v1.34.11+k3s1   +0.02s
kona-worker-1          worker          container   192.168.70.20   running   true    v1.34.11+k3s1   +0.03s
kona-worker-2          worker          container   192.168.70.21   running   true    v1.34.11+k3s1   +0.03s

$ ls -la "~/Library/Application Support/kona/kona/"
-rw-r--r--@ 1 lestex  staff  1100 Sep 26 21:07 cluster.json
-rw-------@ 1 lestex  staff    65 Sep 26 21:07 token
```

Datastore on a block-backed volume (ext4 image attached as virtio-blk, not virtiofs), and the API server reachable from macOS:

```text
$ kona ssh kona-control-plane-1 -- findmnt -no SOURCE,FSTYPE,TARGET /var/lib/rancher/k3s/server/db
/dev/vdc ext4 /var/lib/rancher/k3s/server/db
$ kona ssh kona-control-plane-1 -- ls /var/lib/rancher/k3s/server/db
lost+found
state.db
state.db-shm
state.db-wal
$ curl -sk https://192.168.70.10:6443/livez -o /dev/null -w '%{http_code}\n'     # unauthenticated, from macOS
401
$ kubectl --kubeconfig ~/.kube/kona-kona get --raw /livez
ok
```

Sonobuoy v0.57.5 quick run:

```text
$ sonobuoy run --kubeconfig ~/.kube/kona-kona --mode quick --wait=15
...
21:13:10             e2e                 global   complete   passed   Passed:  0, Failed:  0, Remaining:  1
21:13:10    systemd-logs   kona-control-plane-1   complete   passed
21:13:10    systemd-logs          kona-worker-1   complete   passed
21:13:10    systemd-logs          kona-worker-2   complete   passed
21:13:10 Sonobuoy has completed. Use `sonobuoy retrieve` to get results.

$ sonobuoy results $(sonobuoy retrieve)
Plugin: e2e
Status: passed
Total: 7148
Passed: 5
Failed: 0
Skipped: 7143

Plugin: systemd-logs
Status: passed
Total: 3
Passed: 3
Failed: 0
Skipped: 0

Run Details:
API Server version: v1.34.11+k3s1
Node health: 3/3 (100%)
Pods health: 8/8 (100%)

$ sonobuoy results $(sonobuoy retrieve) --mode detailed --plugin e2e   # non-skipped entries
passed [ReportBeforeSuite]
passed [SynchronizedBeforeSuite]
passed [SynchronizedAfterSuite]
passed [ReportAfterSuite] Kubernetes e2e suite report
passed [It] [sig-node] Pods should be submitted and removed [NodeConformance] [Conformance]
```

(The live progress line reads "Passed: 0 … Remaining: 1" even after completion, a Sonobuoy display quirk. The retrieved results above are authoritative.)

### 1.2 Partial-failure handling: PASS

Forced failure (`--wait 3s`) rolls back with no orphans:

```text
$ kona create cluster rb --workers 1 --memory 1G --wait 3s
• Planned cluster "rb": 1 control plane(s), 1 worker(s) on 192.168.71.0/24
• Creating network kona-rb (192.168.71.0/24)
• Starting rb-control-plane-1 (192.168.71.10, 2 CPU, 1G)
• Waiting for the API server on 192.168.71.10
✗ Create failed, rolling back (use --retain to keep the VMs)
• Deleting node rb-control-plane-1
• Deleting volume rb-control-plane-1-data
error: API server on rb-control-plane-1 not ready before --wait expired: ...
exit=1
$ kona get clusters
NAME   PHASE   STATUS   DISTRO   NODES   SUBNET            ISSUES
kona   ready   ok       k3s      3/3     192.168.70.0/24   -
$ container ls -a | grep -c ' rb-'; container volume ls | grep -c rb-; container network ls | grep -c kona-rb
0
0
0
```

With `--retain`, the cluster is kept and flagged, and re-running `create` resumes it:

```text
$ kona create cluster rb --workers 1 --memory 1G --wait 3s --retain
...
error: API server on rb-control-plane-1 not ready before --wait expired: error: stat /etc/rancher/k3s/k3s.yaml: no such file or directory (see `container logs rb-control-plane-1`)
cluster "rb" retained for debugging; resume with `kona create cluster rb` or remove with `kona delete cluster rb`
$ kona get clusters
NAME   PHASE    STATUS     DISTRO   NODES   SUBNET            ISSUES
kona   ready    ok         k3s      3/3     192.168.70.0/24   -
rb     failed   degraded   k3s      1/2     192.168.71.0/24   rb-worker-1 missing
$ kona create cluster rb --workers 1 --memory 1G   # resume
Resuming cluster "rb" (phase failed)
✓ Node rb-control-plane-1 already running
• Waiting for the API server on 192.168.71.10
• Starting rb-worker-1 (192.168.71.20, 2 CPU, 1G)
• Waiting up to 4m57s for 2 nodes to be Ready
✓ All 2 nodes Ready
✓ Wrote kubeconfig /Users/lestex/.kube/kona-rb (context kona-rb)
✓ Cluster "rb" is ready
```

Delete is idempotent:

```text
$ kona delete cluster rb   # run 1
• Deleting node rb-worker-1
• Deleting node rb-control-plane-1
• Deleting volume rb-control-plane-1-data
✓ Deleted cluster "rb"
$ kona delete cluster rb   # run 2
✓ Deleted cluster "rb"
```

### 1.3 kubeadm path: PASS

Node image `ghcr.io/lestex/kona-node:v1.34.12-kubeadm` (systemd, containerd v2.4.1, runc v1.5.1, kubelet/kubeadm v1.34.12, flannel v0.28.9) was built locally with `make -C images/node build-kubeadm`.

```text
$ time kona create cluster kadm --distro kubeadm --workers 2
• Planned cluster "kadm": 1 control plane(s), 2 worker(s) on 192.168.70.0/24
• Creating network kona-kadm (192.168.70.0/24)
• Starting kadm-control-plane-1 (192.168.70.10, 2 CPU, 2G)
• Waiting for the API server on 192.168.70.10
• Starting kadm-worker-1 (192.168.70.20, 2 CPU, 2G)
• Starting kadm-worker-2 (192.168.70.21, 2 CPU, 2G)
• Waiting up to 4m24s for 3 nodes to be Ready
✓ All 3 nodes Ready
✓ Wrote kubeconfig /Users/lestex/.kube/kona-kadm (context kona-kadm)
✓ Cluster "kadm" is ready
./bin/kona create cluster kadm --distro kubeadm --workers 2 2>&1  0.94s user 0.64s system 3% cpu 46.111 total

$ kubectl --kubeconfig ~/.kube/kona-kadm get nodes -o wide      # from the macOS host
NAME                   STATUS   ROLES           AGE   VERSION    INTERNAL-IP     EXTERNAL-IP   OS-IMAGE                            KERNEL-VERSION   CONTAINER-RUNTIME
kadm-control-plane-1   Ready    control-plane   38s   v1.34.12   192.168.70.10   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona     containerd://2.4.1
kadm-worker-1          Ready    <none>          24s   v1.34.12   192.168.70.20   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona     containerd://2.4.1
kadm-worker-2          Ready    <none>          20s   v1.34.12   192.168.70.21   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona     containerd://2.4.1

$ kona get nodes --name kadm
NAME                   ROLE            KIND        IP              VM        READY   VERSION    CLOCK SKEW
kadm-control-plane-1   control-plane   container   192.168.70.10   running   true    v1.34.12   +0.02s
kadm-worker-1          worker          container   192.168.70.20   running   true    v1.34.12   +0.02s
kadm-worker-2          worker          container   192.168.70.21   running   true    v1.34.12   +0.05s
```

etcd on the block volume. kubeadm's `etcd.local.dataDir` is `/var/lib/etcd/data`, because a fresh ext4 volume has `lost+found` and kubeadm preflight requires an empty directory:

```text
$ kona ssh --name kadm kadm-control-plane-1 -- findmnt -no SOURCE,FSTYPE,TARGET /var/lib/etcd
/dev/vdc ext4 /var/lib/etcd
$ kona ssh --name kadm kadm-control-plane-1 -- ls /var/lib/etcd /var/lib/etcd/data
/var/lib/etcd:
data
lost+found

/var/lib/etcd/data:
member

$ kona ssh --name kadm kadm-control-plane-1 -- etcdctl --endpoints=https://127.0.0.1:2379 \
    --cacert=/etc/kubernetes/pki/etcd/ca.crt --cert=/etc/kubernetes/pki/etcd/healthcheck-client.crt \
    --key=/etc/kubernetes/pki/etcd/healthcheck-client.key check perf      # progress bar stripped
PASS: Throughput is 150 writes/s
PASS: Slowest request took 0.128374s
PASS: Stddev is 0.004452s
PASS
```

Sonobuoy v0.57.5 quick run:

```text
$ sonobuoy results $(sonobuoy retrieve)
Plugin: e2e
Status: passed
Total: 7148
Passed: 5
Failed: 0
Skipped: 7143

Plugin: systemd-logs
Status: passed
Total: 3
Passed: 3
Failed: 0
Skipped: 0

Run Details:
API Server version: v1.34.12
Node health: 3/3 (100%)
Pods health: 17/17 (100%)

$ sonobuoy results $(sonobuoy retrieve) --mode detailed --plugin e2e   # non-skipped entries
passed [ReportBeforeSuite]
passed [SynchronizedBeforeSuite]
passed [SynchronizedAfterSuite]
passed [ReportAfterSuite] Kubernetes e2e suite report
passed [It] [sig-node] Pods should be submitted and removed [NodeConformance] [Conformance]
```

The first attempt failed on that preflight check, and rollback left nothing behind:

```text
✗ Create failed, rolling back (use --retain to keep the VMs)
• Deleting node kadm-control-plane-1
• Deleting volume kadm-control-plane-1-data
error: kubeadm init on kadm-control-plane-1: ... [ERROR DirAvailable--var-lib-etcd]: /var/lib/etcd is not empty
```

### 1.4 k3s regression after the shared entrypoint change: PASS

The entrypoint now makes the static node IP the primary `eth0` address, which flannel `--iface` needs on kubeadm:

```text
$ time kona create cluster --workers 2
✓ Cluster "kona" is ready
./bin/kona create cluster --workers 2 2>&1  0.58s user 0.33s system 6% cpu 13.151 total
$ kubectl --kubeconfig ~/.kube/kona-kona get nodes
NAME                   STATUS   ROLES           AGE   VERSION
kona-control-plane-1   Ready    control-plane   5s    v1.34.11+k3s1
kona-worker-1          Ready    <none>          1s    v1.34.11+k3s1
kona-worker-2          Ready    <none>          1s    v1.34.11+k3s1
$ kona ssh kona-worker-1 -- ip -4 -o addr show eth0
192.168.70.20/24 scope global eth0
192.168.70.3/24 scope global secondary
```

### 1.5 Default moved to Kubernetes 1.37: PASS (both distros)

Re-run on 2026-09-27 after pinning `KUBERNETES_VERSION=v1.37.1`, `K3S_VERSION=v1.37.0+k3s1`, `CRICTL_VERSION=v1.37.0` and `ETCD_VERSION=v3.7.2`, with checksums re-pinned and both images rebuilt. Sections 1.1–1.4 above are the 1.34 runs.

k3s (default):

```text
$ kona version | grep -E 'KUBERNETES|K3S|ETCD|CRICTL'
CRICTL_VERSION              v1.37.0
ETCD_VERSION                v3.7.2
K3S_VERSION                 v1.37.0+k3s1
KUBERNETES_VERSION          v1.37.1

$ time kona create cluster --workers 2
• Planned cluster "kona": 1 control plane(s), 2 worker(s) on 192.168.70.0/24
• Creating network kona-kona (192.168.70.0/24)
• Starting kona-control-plane-1 (192.168.70.10, 2 CPU, 2G)
• Waiting for the API server on 192.168.70.10
• Starting kona-worker-1 (192.168.70.20, 2 CPU, 2G)
• Starting kona-worker-2 (192.168.70.21, 2 CPU, 2G)
• Waiting up to 4m53s for 3 nodes to be Ready
✓ All 3 nodes Ready
✓ Wrote kubeconfig /Users/lestex/.kube/kona-kona (context kona-kona)
✓ Cluster "kona" is ready
./bin/kona create cluster --workers 2 2>&1  0.52s user 0.31s system 6% cpu 12.749 total

$ kubectl --kubeconfig ~/.kube/kona-kona get nodes -o wide
NAME                   STATUS   ROLES           AGE   VERSION        INTERNAL-IP     EXTERNAL-IP   OS-IMAGE                            KERNEL-VERSION         CONTAINER-RUNTIME
kona-control-plane-1   Ready    control-plane   4s    v1.37.0+k3s1   192.168.70.10   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona (arm64)   containerd://2.3.4-k3s1
kona-worker-1          Ready    <none>          1s    v1.37.0+k3s1   192.168.70.20   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona (arm64)   containerd://2.3.4-k3s1
kona-worker-2          Ready    <none>          0s    v1.37.0+k3s1   192.168.70.21   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona (arm64)   containerd://2.3.4-k3s1

$ kona ssh kona-control-plane-1 -- findmnt -no SOURCE,FSTYPE,TARGET /var/lib/rancher/k3s/server/db
/dev/vdc ext4 /var/lib/rancher/k3s/server/db

$ sonobuoy run --mode quick --wait=15 && sonobuoy results $(sonobuoy retrieve)
Plugin: e2e
Status: passed
Total: 3213
Passed: 8
Failed: 0
Skipped: 3205
...
API Server version: v1.37.0+k3s1
Node health: 3/3 (100%)
Pods health: 9/9 (100%)
# non-skipped e2e entries
passed [ReportBeforeSuite]
passed [ReportBeforeSuite]
passed [SynchronizedBeforeSuite]
passed [SynchronizedAfterSuite]
passed [ReportAfterSuite] Invariant Metrics
passed [ReportAfterSuite] Kubernetes e2e suite report
passed [It] [sig-node] Pods should be submitted and removed [Conformance] [NodeConformance]
passed [ReportAfterSuite] [sig-testing] Log Check
```

kubeadm:

```text
$ time kona create cluster kadm --distro kubeadm --workers 2
...
✓ Cluster "kadm" is ready
./bin/kona create cluster kadm --distro kubeadm --workers 2 2>&1  0.97s user 0.67s system 3% cpu 48.994 total

$ kubectl --kubeconfig ~/.kube/kona-kadm get nodes -o wide
NAME                   STATUS   ROLES           AGE   VERSION   INTERNAL-IP     EXTERNAL-IP   OS-IMAGE                            KERNEL-VERSION         CONTAINER-RUNTIME
kadm-control-plane-1   Ready    control-plane   28s   v1.37.1   192.168.71.10   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona (arm64)   containerd://2.4.1
kadm-worker-1          Ready    <none>          15s   v1.37.1   192.168.71.20   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona (arm64)   containerd://2.4.1
kadm-worker-2          Ready    <none>          11s   v1.37.1   192.168.71.21   <none>        Fedora Linux 44 (Container Image)   6.18.35-kona (arm64)   containerd://2.4.1

$ kona ssh --name kadm kadm-control-plane-1 -- findmnt -no SOURCE,FSTYPE,TARGET /var/lib/etcd
/dev/vdc ext4 /var/lib/etcd
$ kona ssh --name kadm kadm-control-plane-1 -- etcdctl ... check perf      # progress bar stripped
PASS: Throughput is 150 writes/s
PASS: Slowest request took 0.025849s
PASS: Stddev is 0.001034s
PASS
$ kubectl -n kube-system get pod -l component=etcd -o jsonpath='{..image}'
registry.k8s.io/etcd:3.7.0-0
$ kona ssh --name kadm kadm-control-plane-1 -- crictl images | grep pause      # only the pinned sandbox
registry.k8s.io/pause                     3.10.2              3884a33719231       268kB

$ sonobuoy results $(sonobuoy retrieve)
Plugin: e2e
Status: passed
Total: 3213
Passed: 8
Failed: 0
Skipped: 3205
...
API Server version: v1.37.1
Node health: 3/3 (100%)
Pods health: 17/17 (100%)
# non-skipped e2e entries: same 8 as k3s above, all passed
```
