// Package driver implements kona-venus-dra: a DRA kubelet plugin that
// exposes the Virtio-GPU Venus device of a krunkit GPU node.
package driver

// DriverName must be a DNS subdomain the project controls.
//
// PLACEHOLDER: "example.invalid" is reserved and can never be registered, so
// this is safe to ship until kona has a domain of its own. Replace it here
// (and in deploy/*.yaml) with venus.kona.<owned-domain>.
const DriverName = "venus.kona.example.invalid"

// CDIKind is the CDI vendor/class for devices this driver injects.
const CDIKind = DriverName + "/gpu"

// Node paths.
const (
	// RenderNode is the DRM render node Venus exposes in krunkit guests.
	RenderNode = "/dev/dri/renderD128"
	// VenusICD is the Vulkan ICD manifest of the patched Mesa venus driver.
	// Pointing VK_DRIVER_FILES at it stops the loader from silently falling
	// back to llvmpipe (docs/gpu.md).
	VenusICD = "/usr/share/vulkan/icd.d/virtio_icd.aarch64.json"
)

// DeviceName is the single device a krunkit node exposes.
const DeviceName = "gpu0"

// MemoryCapacity is the device's consumable capacity: claims share the GPU
// by requesting an amount of it (accounting only; Venus does not isolate).
const MemoryCapacity = "memory"

// MemoryEnv tells a container how much of the GPU memory its claim holds.
const MemoryEnv = "KONA_VENUS_MEMORY"
