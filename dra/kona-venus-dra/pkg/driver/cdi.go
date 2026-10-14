package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// cdiVersion is supported by containerd 2.x.
const cdiVersion = "0.6.0"

type cdiSpec struct {
	CDIVersion string      `json:"cdiVersion"`
	Kind       string      `json:"kind"`
	Devices    []cdiDevice `json:"devices"`
}

type cdiDevice struct {
	Name           string         `json:"name"`
	ContainerEdits containerEdits `json:"containerEdits"`
}

type containerEdits struct {
	Env         []string     `json:"env,omitempty"`
	DeviceNodes []deviceNode `json:"deviceNodes,omitempty"`
}

type deviceNode struct {
	Path string `json:"path"`
}

// CDI manages per-claim CDI spec files. The files are the driver's only
// state: a claim is prepared exactly when its spec file exists.
type CDI struct {
	Dir string
}

func specFileName(claimUID string) string {
	return strings.ReplaceAll(DriverName, "/", "_") + "-" + claimUID + ".json"
}

// cdiDeviceName is unique per claim so containerd never serves a cached
// spec for a different claim.
func cdiDeviceName(claimUID string) string { return claimUID + "-" + DeviceName }

// DeviceID is the fully-qualified CDI device name passed to the runtime.
func DeviceID(claimUID string) string { return CDIKind + "=" + cdiDeviceName(claimUID) }

// Write creates (or rewrites) the spec for a claim that holds memBytes of
// the GPU memory (0: the device has no capacity).
func (c CDI) Write(claimUID string, gpu VenusGPU, memBytes int64) error {
	env := []string{
		"VK_DRIVER_FILES=" + VenusICD,
		"KONA_VENUS_DEVICE=" + gpu.DeviceName,
	}
	if memBytes > 0 {
		env = append(env, fmt.Sprintf("%s=%d", MemoryEnv, memBytes))
	}
	spec := cdiSpec{
		CDIVersion: cdiVersion,
		Kind:       CDIKind,
		Devices: []cdiDevice{{
			Name: cdiDeviceName(claimUID),
			ContainerEdits: containerEdits{
				DeviceNodes: []deviceNode{{Path: RenderNode}},
				Env:         env,
			},
		}},
	}
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(c.Dir, "."+specFileName(claimUID)+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(c.Dir, specFileName(claimUID)))
}

// Memory returns how much GPU memory a prepared claim holds. Specs without
// the entry (written before sharing existed) held the whole device.
func (c CDI) Memory(claimUID string, capBytes int64) (int64, error) {
	b, err := os.ReadFile(filepath.Join(c.Dir, specFileName(claimUID)))
	if err != nil {
		return 0, err
	}
	var spec cdiSpec
	if err := json.Unmarshal(b, &spec); err != nil {
		return 0, fmt.Errorf("CDI spec of claim %s: %w", claimUID, err)
	}
	for _, d := range spec.Devices {
		for _, e := range d.ContainerEdits.Env {
			if v, ok := strings.CutPrefix(e, MemoryEnv+"="); ok {
				return strconv.ParseInt(v, 10, 64)
			}
		}
	}
	return capBytes, nil
}

// Remove deletes a claim's spec; a missing spec is not an error.
func (c CDI) Remove(claimUID string) error {
	err := os.Remove(filepath.Join(c.Dir, specFileName(claimUID)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Prepared returns the claim UIDs that currently have spec files.
func (c CDI) Prepared() ([]string, error) {
	entries, err := os.ReadDir(c.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	prefix := strings.ReplaceAll(DriverName, "/", "_") + "-"
	var uids []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".json") {
			uids = append(uids, strings.TrimSuffix(strings.TrimPrefix(n, prefix), ".json"))
		}
	}
	return uids, nil
}

// errInUse is returned when the single device is prepared for another claim.
func errInUse(other string) error {
	return fmt.Errorf("device %s is already prepared for claim %s", DeviceName, other)
}

// errNoMemory is returned when a claim needs more GPU memory than is free.
func errNoMemory(need, free int64) error {
	return fmt.Errorf("device %s has %s of memory free, claim needs %s", DeviceName,
		resource.NewQuantity(free, resource.BinarySI), resource.NewQuantity(need, resource.BinarySI))
}
