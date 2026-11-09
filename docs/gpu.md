# GPU nodes: krunkit + Venus

## Verified stack (Phase 0)

| Layer | Version | Notes |
|-------|---------|-------|
| Host VMM | krunkit 1.3.2 / libkrun 1.19.5 / virglrenderer-krun 0.10.4e / MoltenVK 1.4.2 | `brew install libkrun/krun/krunkit` |
| Guest OS | Fedora 44 Cloud Base Generic aarch64, kernel 6.19.10-300.fc44 | EFI boot from the first `virtio-blk` |
| Guest Mesa | **25.3.6-102.fc44** from COPR `slp/mesa-libkrun-vulkan` | Upstream Fedora Mesa (26.2.3, 26.0.3) does **not** work. See below. |
| Result | `Virtio-GPU Venus (Apple M3 Pro)`, `driverName = venus`, Vulkan API 1.2.0 | [`spikes/02-krunkit/vulkaninfo.txt`](../spikes/02-krunkit/vulkaninfo.txt) |

## Findings

- **Venus is on unconditionally** in krunkit. There is no flag, and
  `--device virtio-gpu,...` is parsed and then ignored. The guest sees
  `virtio_gpu` with `+virgl +resource_blob +host_visible +context_init` and a
  Venus capset (id 4), and gets `/dev/dri/card0` plus `/dev/dri/renderD128`
  (mode 0666, group `render`).
- **Upstream Mesa fails.** With Fedora's `mesa-vulkan-drivers` 26.x,
  `vkCreateInstance` returns `VK_ERROR_OUT_OF_HOST_MEMORY`, and the guest
  kernel logs `response 0x1200 (command 0x208/0x209)`. Those commands are
  `RESOURCE_MAP_BLOB`/`UNMAP_BLOB`, which the host rejects. The libkrun
  maintainers' patched Mesa (COPR `slp/mesa-libkrun-vulkan`, which also
  builds for Fedora 43) fixes it. Lima documents the same workaround. kona
  pins the exact NVR in `versions.env` (`MESA_VERSION`) and bakes it into the
  GPU node image. dnf `versionlock` keeps upgrades from replacing it.
- **Two Vulkan devices are visible**: Venus (INTEGRATED_GPU) and llvmpipe
  (CPU). The DRA driver must publish only devices with `driverName == venus`,
  and the CDI spec should set `VK_DRIVER_FILES` to the virtio ICD so
  workloads don't silently fall back to llvmpipe.
- **Reported API version is 1.2.0** (Venus over MoltenVK), not the loader's
  1.4.x. That is the value published as the DRA attribute `vulkanApiVersion`.
- The Dozen ICD (`dzn`) logs a harmless loader warning. The GPU image removes
  the unused ICD JSONs.

## DRA attribute mapping (for Phase 3)

| Attribute | Source | Observed value |
|-----------|--------|----------------|
| `driver` | constant | `venus` |
| `deviceName` | `vulkaninfo --summary` → `deviceName` | `Virtio-GPU Venus (Apple M3 Pro)` |
| `vulkanApiVersion` | `vulkaninfo --summary` → `apiVersion` | `1.2.0` (published as a semver `version` attribute) |

## Sharing the GPU by memory

A krunkit node has one Venus device, `gpu0`. Its ResourceSlice publishes a
consumable `memory` capacity and `allowMultipleAllocations: true`
(`DRAConsumableCapacity`, beta and on by default in Kubernetes 1.37):

| Field | Value | Source |
|-------|-------|--------|
| `capacity.memory.value` | Venus device-local heap, rounded down to whole GiB (`18Gi` on an 18 GiB M3 Pro) | full `vulkaninfo` → `memoryHeaps[]` with `MEMORY_HEAP_DEVICE_LOCAL_BIT` |
| `requestPolicy.default` | the whole capacity | so a claim with no amount is exclusive, as before |
| `requestPolicy.validRange` | min `1Gi`, step `1Gi` | |

Venus reports the host's unified memory as its heap, so the capacity is
"what one app could allocate", not memory set aside for this VM. If the heap
can't be read, the driver exits instead of publishing a device it can't
account for.

A claim asks for an amount through `capacity.requests` (see
`examples/vulkan-compute/vulkan-shared.yaml`):

```yaml
exactly:
  deviceClassName: kona-gpu
  capacity:
    requests:
      memory: 8Gi
```

The scheduler places claims until the capacity is used up. The amount
appears in the container as `KONA_VENUS_MEMORY` (bytes). It is **accounting,
not isolation**: Venus doesn't limit what a pod allocates, so a pod can use
more than it claimed. As a last check, the driver refuses to prepare a claim
that would overcommit the device. Each claim's amount is recorded in its CDI
spec, and specs from v0.1.0 (which have no amount) count as the whole device.

Verified on the `kona` cluster (driver v0.2.0): three replicas each claiming
8Gi of 18Gi. Two ran the compute test on the GPU at the same time and the
third stayed Pending:

```
vulkan-shared-…-cqh68   0/1  Pending   <none>
vulkan-shared-…-d8q4f   1/1  Running   kona-gpu-worker-1
vulkan-shared-…-prm4t   1/1  Running   kona-gpu-worker-1
…-d8q4f-gpu-4flmv: gpu0 consumed={"memory":"8Gi"} share=df25ce5c-…
…-prm4t-gpu-sgm2k: gpu0 consumed={"memory":"8Gi"} share=20034176-…
KONA_VENUS_MEMORY=8589934592
PASS: 1048576 elements computed on the GPU and verified (9.80 ms)
PASS: 1048576 elements computed on the GPU and verified (10.16 ms)
FailedScheduling  0/3 nodes are available: 3 cannot allocate all claims.
```

While the two 8Gi pods ran, a claim with no amount (`examples/vulkaninfo.yaml`)
stayed Pending. It ran once they were deleted. No CDI specs were left over,
and `hack/gate-dra.sh` (exclusive claims) still passes.
