package driver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

func TestParseVenusHeapBytes(t *testing.T) {
	// Trimmed real `vulkaninfo` from a krunkit guest on an 18 GiB M3 Pro.
	b, err := os.ReadFile("testdata/vulkaninfo.txt")
	if err != nil {
		t.Fatal(err)
	}
	full := string(b)
	if n, err := ParseVenusHeapBytes(full); err != nil || n != 19327352832 {
		t.Fatalf("venus first: %d, %v", n, err)
	}
	// llvmpipe listed first must not leak its heap into the result.
	i := strings.Index(full, "GPU1:")
	if n, err := ParseVenusHeapBytes(full[i:] + full[:i]); err != nil || n != 19327352832 {
		t.Fatalf("venus second: %d, %v", n, err)
	}
	if _, err := ParseVenusHeapBytes(full[i:]); err == nil {
		t.Fatal("llvmpipe alone must not count")
	}
}

func memClaim(uid, mem string) *resourceapi.ResourceClaim {
	c := claim(uid)
	if mem != "" {
		c.Status.Allocation.Devices.Results[0].ConsumedCapacity = map[resourceapi.QualifiedName]resource.Quantity{
			MemoryCapacity: resource.MustParse(mem),
		}
	}
	return c
}

func TestResourcesCapacity(t *testing.T) {
	d := &Driver{GPU: VenusGPU{DeviceName: "Venus", APIVersion: "1.2", HeapBytes: 19327352832 + 123}}
	dev := d.Resources("n").Pools["n"].Slices[0].Devices[0]
	c, ok := dev.Capacity[MemoryCapacity]
	if !ok || dev.AllowMultipleAllocations == nil || !*dev.AllowMultipleAllocations {
		t.Fatalf("device not shareable: %+v", dev)
	}
	p := c.RequestPolicy
	if c.Value.String() != "18Gi" || p.Default.String() != "18Gi" ||
		p.ValidRange.Min.String() != "1Gi" || p.ValidRange.Step.String() != "1Gi" || p.ValidRange.Max != nil {
		t.Fatalf("capacity: %+v %+v", c, p.ValidRange)
	}
	d.GPU.HeapBytes = 0
	if dev := d.Resources("n").Pools["n"].Slices[0].Devices[0]; dev.Capacity != nil || dev.AllowMultipleAllocations != nil {
		t.Fatalf("unknown heap must stay exclusive: %+v", dev)
	}
}

func TestPrepareSharesMemory(t *testing.T) {
	ctx := context.Background()
	d := &Driver{GPU: VenusGPU{DeviceName: "Venus", APIVersion: "1.2.0", HeapBytes: 8 * gib}, CDI: CDI{Dir: t.TempDir()}}
	prep := func(c *resourceapi.ResourceClaim) error {
		res, err := d.PrepareResourceClaims(ctx, []*resourceapi.ResourceClaim{c})
		if err != nil {
			t.Fatal(err)
		}
		return res[c.UID].Err
	}
	for _, uid := range []string{"a", "b"} {
		if err := prep(memClaim(uid, "4Gi")); err != nil {
			t.Fatalf("claim %s: %v", uid, err)
		}
	}
	if n, err := d.CDI.Memory("a", 8*gib); err != nil || n != 4*gib {
		t.Fatalf("memory of a: %d %v", n, err)
	}
	if err := prep(memClaim("a", "4Gi")); err != nil {
		t.Fatalf("re-prepare must not count itself: %v", err)
	}
	if err := prep(memClaim("c", "1Gi")); err == nil || !strings.Contains(err.Error(), "0 of memory free, claim needs 1Gi") {
		t.Fatalf("overcommit: %v", err)
	}
	if _, err := d.UnprepareResourceClaims(ctx, []kubeletplugin.NamespacedObject{{UID: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := prep(memClaim("c", "1Gi")); err != nil {
		t.Fatalf("after unprepare: %v", err)
	}
	// Allocated without consumed capacity: holds the whole device.
	if err := prep(memClaim("d", "")); err == nil {
		t.Fatal("claim without consumed capacity fit next to others")
	}
}

func TestMemoryOfSpecWithoutEntryIsWholeDevice(t *testing.T) {
	c := CDI{Dir: t.TempDir()}
	if err := c.Write("old", VenusGPU{DeviceName: "Venus"}, 0); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Memory("old", 8*gib); err != nil || n != 8*gib {
		t.Fatalf("got %d %v", n, err)
	}
}
