package driver

import (
	"context"
	"errors"
	"sync"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

// Driver implements kubeletplugin.DRAPlugin for one Venus GPU.
type Driver struct {
	GPU VenusGPU
	CDI CDI
	// Cancel stops the process on fatal background errors.
	Cancel context.CancelCauseFunc

	mu sync.Mutex
}

var _ kubeletplugin.DRAPlugin = (*Driver)(nil)

// Resources returns the node's ResourceSlice content: one pool named after
// the node with the single Venus device. When the heap size is known the
// device is shareable: claims consume its "memory" capacity in 1 GiB steps,
// and a claim that asks for no amount gets all of it (exclusive).
func (d *Driver) Resources(nodeName string) resourceslice.DriverResources {
	dev := resourceapi.Device{
		Name: DeviceName,
		Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			"driver":           {StringValue: ptr.To("venus")},
			"deviceName":       {StringValue: ptr.To(d.GPU.DeviceName)},
			"vulkanApiVersion": {VersionValue: ptr.To(SemverAPIVersion(d.GPU.APIVersion))},
		},
	}
	if capBytes := d.memoryCapacity(); capBytes > 0 {
		all := *resource.NewQuantity(capBytes, resource.BinarySI)
		step := *resource.NewQuantity(gib, resource.BinarySI)
		dev.AllowMultipleAllocations = ptr.To(true)
		dev.Capacity = map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
			MemoryCapacity: {Value: all, RequestPolicy: &resourceapi.CapacityRequestPolicy{
				Default:    &all,
				ValidRange: &resourceapi.CapacityRequestPolicyRange{Min: &step, Step: &step},
			}},
		}
	}
	return resourceslice.DriverResources{Pools: map[string]resourceslice.Pool{
		nodeName: {Slices: []resourceslice.Slice{{Devices: []resourceapi.Device{dev}}}},
	}}
}

const gib = 1 << 30

// memoryCapacity is the Venus heap rounded down to whole GiB, so 1 GiB
// steps divide it exactly; 0 means unknown, and the device is exclusive.
func (d *Driver) memoryCapacity() int64 { return d.GPU.HeapBytes / gib * gib }

// claimMemory is what a claim consumes of the device. A result without
// consumed capacity (allocated without DRAConsumableCapacity) takes it all.
func claimMemory(claim *resourceapi.ResourceClaim, capBytes int64) int64 {
	var n int64
	for _, r := range claim.Status.Allocation.Devices.Results {
		if r.Driver != DriverName {
			continue
		}
		q, ok := r.ConsumedCapacity[MemoryCapacity]
		if !ok {
			return capBytes
		}
		n += q.Value()
	}
	return n
}

// PrepareResourceClaims implements kubeletplugin.DRAPlugin.
func (d *Driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	logger := klog.FromContext(ctx)
	out := make(map[types.UID]kubeletplugin.PrepareResult, len(claims))
	for _, claim := range claims {
		out[claim.UID] = d.prepare(claim)
		if err := out[claim.UID].Err; err != nil {
			logger.Error(err, "prepare failed", "claim", klog.KObj(claim))
		} else {
			logger.Info("prepared", "claim", klog.KObj(claim), "cdi", DeviceID(string(claim.UID)))
		}
	}
	return out, nil
}

func (d *Driver) prepare(claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	if claim.Status.Allocation == nil {
		return kubeletplugin.PrepareResult{Err: errors.New("claim is not allocated")}
	}
	uid := string(claim.UID)
	var devs []kubeletplugin.Device
	for _, r := range claim.Status.Allocation.Devices.Results {
		if r.Driver != DriverName {
			continue
		}
		devs = append(devs, kubeletplugin.Device{
			Requests: []string{r.Request}, PoolName: r.Pool, DeviceName: r.Device,
			CDIDeviceIDs: []string{DeviceID(uid)},
		})
	}
	if len(devs) == 0 {
		return kubeletplugin.PrepareResult{}
	}
	// Last line of defence against the scheduler overcommitting the device.
	prepared, err := d.CDI.Prepared()
	if err != nil {
		return kubeletplugin.PrepareResult{Err: err}
	}
	capBytes := d.memoryCapacity()
	need := claimMemory(claim, capBytes)
	var used int64
	for _, other := range prepared {
		if other == uid {
			continue
		}
		if capBytes == 0 {
			return kubeletplugin.PrepareResult{Err: errInUse(other)}
		}
		n, err := d.CDI.Memory(other, capBytes)
		if err != nil {
			return kubeletplugin.PrepareResult{Err: err}
		}
		used += n
	}
	if capBytes > 0 && used+need > capBytes {
		return kubeletplugin.PrepareResult{Err: errNoMemory(need, capBytes-used)}
	}
	if err := d.CDI.Write(uid, d.GPU, need); err != nil {
		return kubeletplugin.PrepareResult{Err: err}
	}
	return kubeletplugin.PrepareResult{Devices: devs}
}

// UnprepareResourceClaims implements kubeletplugin.DRAPlugin.
func (d *Driver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[types.UID]error, len(claims))
	for _, c := range claims {
		out[c.UID] = d.CDI.Remove(string(c.UID))
		klog.FromContext(ctx).Info("unprepared", "claim", c.NamespacedName, "err", out[c.UID])
	}
	return out, nil
}

// HandleError implements kubeletplugin.DRAPlugin.
func (d *Driver) HandleError(ctx context.Context, err error, msg string) {
	runtime.HandleErrorWithContext(ctx, err, msg)
	if !errors.Is(err, kubeletplugin.ErrRecoverable) && d.Cancel != nil {
		d.Cancel(err)
	}
}

// WatchHealthStatus implements kubeletplugin.DRAPlugin (not supported).
func (d *Driver) WatchHealthStatus(context.Context, chan<- kubeletplugin.DeviceHealthReport) error {
	return kubeletplugin.ErrHealthNotSupported
}
