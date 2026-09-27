package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func tarball(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestEnsureDownloadsVerifiesAndCaches(t *testing.T) {
	body := tarball(t, "cilium", "#!/bin/sh\necho hi\n")
	sum := sha256.Sum256(body)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Write(body)
	}))
	defer srv.Close()

	c := &Cache{Dir: t.TempDir()}
	tool := Tool{Name: "cilium", Version: "v1", URL: srv.URL, SHA256: hex.EncodeToString(sum[:])}
	for range 2 {
		p, err := c.Ensure(context.Background(), tool)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		fi, _ := os.Stat(p)
		if !strings.Contains(string(b), "echo hi") || fi.Mode().Perm() != 0o755 {
			t.Fatalf("extracted %q mode %v", b, fi.Mode())
		}
	}
	if hits != 1 {
		t.Fatalf("downloaded %d times, want 1 (cached)", hits)
	}
}

func TestEnsureRejectsChecksumMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(tarball(t, "cilium", "evil"))
	}))
	defer srv.Close()
	c := &Cache{Dir: t.TempDir()}
	tool := Tool{Name: "cilium", Version: "v1", URL: srv.URL, SHA256: strings.Repeat("0", 64)}
	if _, err := c.Ensure(context.Background(), tool); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("want mismatch error, got %v", err)
	}
	if _, err := os.Stat(c.Path(tool)); !os.IsNotExist(err) {
		t.Fatal("unverified binary was installed")
	}
}
