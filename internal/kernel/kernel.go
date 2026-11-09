// Package kernel installs the kona guest kernel: the exact build pinned in
// versions.env (KONA_KERNEL_SHA256_ARM64), downloaded from the GitHub
// release .github/workflows/kernel.yml publishes, or copied from a local
// `make -C kernel` build. Either way the file is checked against the pin
// before it replaces anything.
package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/lestex/kona/internal/version"
)

// Releases is where the kernel releases live.
const Releases = "https://github.com/lestex/kona/releases/download/"

// Name is the kernel's file name, e.g. vmlinux-6.18.35-kona.
func Name() string { return "vmlinux-" + version.Get("KERNEL_VERSION") + "-kona" }

// Pin is the sha256 this kona expects of its kernel ("" in a build without
// the version matrix).
func Pin() string { return version.Checksum("KONA_KERNEL_SHA256_ARM64") }

// Tag is the GitHub release that holds the pinned kernel. The sha prefix
// makes a config change without a version bump a new release rather than a
// replaced asset.
func Tag(pin string) string {
	return "kernel-" + version.Get("KERNEL_VERSION") + "-" + pin[:12]
}

// URL is the pinned kernel's download URL.
func URL(pin string) string { return Releases + Tag(pin) + "/" + Name() }

// Installed reports whether path already holds the pinned kernel.
func Installed(path, pin string) bool {
	got, err := fileSHA256(path)
	return err == nil && got == pin
}

// Install writes src to path if its sha256 is pin. It writes beside path
// and renames, so a failed or mismatched copy never replaces a kernel.
func Install(path string, src io.Reader, pin string) error {
	if pin == "" {
		return errors.New("this kona build has no kernel pin (build it with `make build`)")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, h), src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write kernel: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != pin {
		return fmt.Errorf("kernel sha256 %s does not match the pin %s", got, pin)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Download fetches url. The caller closes the body.
func Download(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
