package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kernels", "vmlinux-test-kona")
	if err := Install(path, strings.NewReader("kernel"), sum("kernel")); err != nil {
		t.Fatal(err)
	}
	if !Installed(path, sum("kernel")) {
		t.Fatal("installed kernel does not match its pin")
	}
}

func TestInstallMismatchKeepsOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vmlinux")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Install(path, strings.NewReader("tampered"), sum("kernel"))
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("want a mismatch error, got %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "old" {
		t.Fatalf("old kernel replaced: %q", b)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("temp file left behind: %v", ents)
	}
}

func TestInstallNoPin(t *testing.T) {
	if err := Install(filepath.Join(t.TempDir(), "k"), strings.NewReader("x"), ""); err == nil {
		t.Fatal("want an error without a pin")
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ok" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("kernel"))
	}))
	defer srv.Close()
	body, err := Download(context.Background(), srv.Client(), srv.URL+"/ok")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	path := filepath.Join(t.TempDir(), "k")
	if err := Install(path, body, sum("kernel")); err != nil {
		t.Fatal(err)
	}
	if _, err := Download(context.Background(), srv.Client(), srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want a 404 error, got %v", err)
	}
}
