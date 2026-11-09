package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Write creates (or rewrites) the spec for a claim.
func (c CDI) Write(claimUID string, gpu VenusGPU) error {
	spec := cdiSpec{
		CDIVersion: cdiVersion,
		Kind:       CDIKind,
		Devices: []cdiDevice{{
			Name: cdiDeviceName(claimUID),
			ContainerEdits: containerEdits{
				DeviceNodes: []deviceNode{{Path: RenderNode}},
				Env: []string{
					"VK_DRIVER_FILES=" + VenusICD,
					"KONA_VENUS_DEVICE=" + gpu.DeviceName,
				},
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
