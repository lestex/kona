package driver

import (
	"fmt"
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

// SemverAPIVersion normalises "1.2" or "1.2.0" to a semver string.
func SemverAPIVersion(v string) string {
	parts := strings.Split(v, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts[:3], ".")
}
