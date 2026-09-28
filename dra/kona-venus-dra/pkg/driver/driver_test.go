package driver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

// Real output from a krunkit guest (spikes/02-krunkit/vulkaninfo.txt).
const summary = `Devices:
========
GPU0:
	apiVersion         = 1.2.0
	driverVersion      = 25.3.6
	deviceType         = PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU
	deviceName         = Virtio-GPU Venus (Apple M3 Pro)
	driverID           = DRIVER_ID_MESA_VENUS
	driverName         = venus
GPU1:
	apiVersion         = 1.4.328
	deviceName         = llvmpipe (LLVM 22.1.0, 128 bits)
	driverName         = llvmpipe
`

func TestParseVulkanSummary(t *testing.T) {
	g, err := ParseVulkanSummary(summary)
	if err != nil || g.DeviceName != "Virtio-GPU Venus (Apple M3 Pro)" || g.APIVersion != "1.2.0" {
		t.Fatalf("got %+v, %v", g, err)
	}
	if _, err := ParseVulkanSummary("GPU0:\n\tapiVersion = 1.4.328\n\tdeviceName = llvmpipe\n\tdriverName = llvmpipe\n"); err == nil {
		t.Fatal("llvmpipe must not count as Venus")
	}
	if SemverAPIVersion("1.2") != "1.2.0" || SemverAPIVersion("1.2.0") != "1.2.0" {
		t.Fatal("SemverAPIVersion")
	}
}

func claim(uid string) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "c-" + uid, Namespace: "default", UID: types.UID(uid)},
		Status: resourceapi.ResourceClaimStatus{Allocation: &resourceapi.AllocationResult{
			Devices: resourceapi.DeviceAllocationResult{Results: []resourceapi.DeviceRequestAllocationResult{
				{Request: "gpu", Driver: DriverName, Pool: "node-1", Device: DeviceName},
			}},
		}},
	}
}

func TestPrepareWritesCDISpecAndUnprepareRemovesIt(t *testing.T) {
	dir := t.TempDir()
	d := &Driver{GPU: VenusGPU{DeviceName: "Venus", APIVersion: "1.2.0"}, CDI: CDI{Dir: dir}}
	ctx := context.Background()
	res, err := d.PrepareResourceClaims(ctx, []*resourceapi.ResourceClaim{claim("uid-1")})
	if err != nil || res["uid-1"].Err != nil {
		t.Fatalf("prepare: %v %v", err, res["uid-1"].Err)
	}
	devs := res["uid-1"].Devices
	if len(devs) != 1 || devs[0].CDIDeviceIDs[0] != "venus.kona.example.invalid/gpu=uid-1-gpu0" || devs[0].PoolName != "node-1" {
		t.Fatalf("devices: %+v", devs)
	}
	b, err := os.ReadFile(filepath.Join(dir, "venus.kona.example.invalid-uid-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec cdiSpec
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Kind != CDIKind || spec.Devices[0].ContainerEdits.DeviceNodes[0].Path != RenderNode ||
		spec.Devices[0].ContainerEdits.Env[0] != "VK_DRIVER_FILES="+VenusICD {
		t.Fatalf("spec: %s", b)
	}
	// Idempotent re-prepare of the same claim.
	if res, _ := d.PrepareResourceClaims(ctx, []*resourceapi.ResourceClaim{claim("uid-1")}); res["uid-1"].Err != nil {
		t.Fatalf("re-prepare: %v", res["uid-1"].Err)
	}
	// The single device cannot be prepared for a second claim.
	if res, _ := d.PrepareResourceClaims(ctx, []*resourceapi.ResourceClaim{claim("uid-2")}); res["uid-2"].Err == nil {
		t.Fatal("second claim prepared on a single-device node")
	}
	for range 2 { // idempotent
		un, err := d.UnprepareResourceClaims(ctx, []kubeletplugin.NamespacedObject{{UID: "uid-1"}})
		if err != nil || un["uid-1"] != nil {
			t.Fatalf("unprepare: %v %v", err, un["uid-1"])
		}
	}
	if left, _ := d.CDI.Prepared(); len(left) != 0 {
		t.Fatalf("CDI specs left: %v", left)
	}
}

func TestResources(t *testing.T) {
	d := &Driver{GPU: VenusGPU{DeviceName: "Virtio-GPU Venus (Apple M3 Pro)", APIVersion: "1.2"}}
	r := d.Resources("kona-gpu-worker-1")
	dev := r.Pools["kona-gpu-worker-1"].Slices[0].Devices[0]
	if dev.Name != DeviceName || *dev.Attributes["driver"].StringValue != "venus" ||
		*dev.Attributes["vulkanApiVersion"].VersionValue != "1.2.0" ||
		*dev.Attributes["deviceName"].StringValue != "Virtio-GPU Venus (Apple M3 Pro)" {
		t.Fatalf("device: %+v", dev)
	}
}
