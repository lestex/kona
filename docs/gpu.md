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
