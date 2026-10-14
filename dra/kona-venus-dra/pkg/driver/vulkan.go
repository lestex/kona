package driver

import (
	"fmt"
	"strconv"
	"strings"
)

// VenusGPU is what vulkaninfo reports for the Venus device.
type VenusGPU struct {
	DeviceName string
	APIVersion string // e.g. "1.2.0"
}

// ParseVulkanSummary extracts the Venus device from `vulkaninfo --summary`
// output. Other devices (llvmpipe) are ignored.
func ParseVulkanSummary(out string) (VenusGPU, error) {
	var cur VenusGPU
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "GPU") && strings.HasSuffix(line, ":") {
			cur = VenusGPU{}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "apiVersion":
			cur.APIVersion = v
		case "deviceName":
			cur.DeviceName = v
		case "driverName":
			if v == "venus" && cur.DeviceName != "" && cur.APIVersion != "" {
				return cur, nil
			}
		}
	}
	return VenusGPU{}, fmt.Errorf("no Venus device in vulkaninfo output")
}

// ParseVenusHeapBytes returns the total size of the Venus device's
// device-local memory heaps from full `vulkaninfo` output. Venus reports
// the host's unified memory; it is the most an app can allocate.
func ParseVenusHeapBytes(out string) (int64, error) {
	var (
		venus, local bool
		size, total  int64
		inHeap       bool
	)
	flush := func() {
		if inHeap && local {
			total += size
		}
		inHeap, local, size = false, false, 0
	}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(raw, "GPU") && strings.HasSuffix(line, ":"):
			flush()
			if venus && total > 0 {
				return total, nil
			}
			venus, total = false, 0
		case strings.HasPrefix(line, "memoryHeaps[") && strings.HasSuffix(line, ":"):
			flush()
			inHeap = true
		case strings.HasPrefix(line, "memoryTypes:"):
			flush()
		case line == "MEMORY_HEAP_DEVICE_LOCAL_BIT":
			local = inHeap
		default:
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch strings.TrimSpace(k) {
			case "driverName":
				venus = strings.TrimSpace(v) == "venus"
			case "size":
				if inHeap {
					f := strings.Fields(v)
					if len(f) > 0 {
						size, _ = strconv.ParseInt(f[0], 10, 64)
					}
				}
			}
		}
	}
	flush()
	if venus && total > 0 {
		return total, nil
	}
	return 0, fmt.Errorf("no device-local memory heap for the Venus device in vulkaninfo output")
}

// SemverAPIVersion normalises "1.2" or "1.2.0" to a semver string.
func SemverAPIVersion(v string) string {
	parts := strings.Split(v, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts[:3], ".")
}
