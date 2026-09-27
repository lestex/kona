// Package tools fetches pinned helper binaries (e.g. the Cilium CLI) into
// kona's cache, verifying them against sha256 sums from versions.env.
package tools

import (
	"archive/tar"
	"compress/gzip"
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

// Tool describes one downloadable binary shipped in a .tar.gz.
type Tool struct {
	Name    string // binary name inside the archive
	Version string
	URL     string
	SHA256  string
}

// CiliumCLI is the pinned Cilium CLI for Apple silicon macOS.
func CiliumCLI() Tool {
	v := version.Get("CILIUM_CLI_VERSION")
	return Tool{
		Name:    "cilium",
		Version: v,
		URL:     "https://github.com/cilium/cilium-cli/releases/download/" + v + "/cilium-darwin-arm64.tar.gz",
		SHA256:  version.Checksum("CILIUM_CLI_SHA256_DARWIN_ARM64"),
	}
}

// Cache stores tools under Dir as <name>-<version>.
type Cache struct {
	Dir    string
	Client *http.Client
}

// Path returns where t is cached.
func (c *Cache) Path(t Tool) string {
	return filepath.Join(c.Dir, t.Name+"-"+t.Version)
}

// Ensure returns the path to t, downloading and verifying it if needed.
func (c *Cache) Ensure(ctx context.Context, t Tool) (string, error) {
	dst := c.Path(t)
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	if t.Version == "" || t.SHA256 == "" {
		return "", fmt.Errorf("%s: version or checksum not pinned in this build", t.Name)
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", err
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", t.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", t.URL, resp.Status)
	}
	archive, err := os.CreateTemp(c.Dir, ".dl-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(archive, h), resp.Body); err != nil {
		return "", fmt.Errorf("download %s: %w", t.URL, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != t.SHA256 {
		return "", fmt.Errorf("%s: sha256 mismatch: got %s, pinned %s", t.URL, got, t.SHA256)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if err := extract(archive, t.Name, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// extract copies the member named name from a .tar.gz to dst (0755).
func extract(r io.Reader, name, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s not found in archive", name)
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) != name || hdr.Typeflag != tar.TypeReg {
			continue
		}
		tmp := dst + ".tmp"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, dst)
	}
}
