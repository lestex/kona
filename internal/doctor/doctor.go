// Package doctor checks host prerequisites and explains how to fix them.
package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/lestex/kona/internal/execx"
	"github.com/lestex/kona/internal/version"
)

// Status of one check.
type Status string

// Check outcomes.
const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Result is one check's outcome.
type Result struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

// Options select optional checks.
type Options struct {
	// Kernel is the kona kernel path to verify ("" = skip).
	Kernel string
}

// Doctor runs checks with injectable host access.
type Doctor struct {
	Runner execx.Runner
	GOOS   string
	GOARCH string
	Stat   func(string) (os.FileInfo, error)
	// Hash returns a file's sha256 (nil skips the kernel pin check).
	Hash func(string) (string, error)
}

// New returns a Doctor for the real host.
func New(r execx.Runner) *Doctor {
	return &Doctor{Runner: r, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Stat: os.Stat, Hash: FileSHA256}
}

// Run executes every check.
func (d *Doctor) Run(ctx context.Context, o Options) []Result {
	rs := []Result{d.macOS(ctx), d.arch()}
	rs = append(rs, d.container(ctx)...)
	if o.Kernel != "" {
		rs = append(rs, d.kernel(o.Kernel))
	}
	rs = append(rs, d.dns(ctx))
	return rs
}

// Failed reports whether any result failed.
func Failed(rs []Result) bool {
	for _, r := range rs {
		if r.Status == Fail {
			return true
		}
	}
	return false
}

func (d *Doctor) macOS(ctx context.Context) Result {
	r := Result{Name: "macos"}
	if d.GOOS != "darwin" {
		r.Status, r.Message = Fail, "kona runs on macOS only (found "+d.GOOS+")"
		return r
	}
	out, err := d.Runner.Run(ctx, "sw_vers", "-productVersion")
	if err != nil {
		r.Status, r.Message = Fail, "cannot read macOS version: "+err.Error()
		return r
	}
	v := strings.TrimSpace(string(out))
	if compare(v, "26") < 0 {
		r.Status = Fail
		r.Message = "macOS " + v + " is not supported: kona needs macOS 26 (Tahoe). On macOS 15, " +
			"`container` networks are not reachable between VMs"
		r.Fix = "upgrade to macOS 26"
		return r
	}
	r.Status, r.Message = OK, "macOS "+v
	return r
}

func (d *Doctor) arch() Result {
	if d.GOARCH != "arm64" {
		return Result{Name: "arch", Status: Fail, Message: "Apple silicon (arm64) required, found " + d.GOARCH}
	}
	return Result{Name: "arch", Status: OK, Message: "arm64"}
}

var versionRE = regexp.MustCompile(`(\d+\.\d+(\.\d+)?)`)

func (d *Doctor) container(ctx context.Context) []Result {
	want := version.Get("CONTAINER_VERSION")
	r := Result{Name: "container"}
	out, err := d.Runner.Run(ctx, "container", "--version")
	if err != nil {
		r.Status, r.Message = Fail, "Apple `container` is not installed"
		r.Fix = "brew install container && container system start"
		return []Result{r}
	}
	got := versionRE.FindString(string(out))
	if want != "" && compare(got, want) < 0 {
		r.Status, r.Message = Fail, fmt.Sprintf("container %s is older than the supported %s", got, want)
		r.Fix = "brew upgrade container && container system stop && container system start"
		return []Result{r}
	}
	r.Status, r.Message = OK, "container "+got

	s := Result{Name: "container-system"}
	status, err := d.Runner.Run(ctx, "container", "system", "status")
	if err != nil {
		s.Status, s.Message = Fail, "`container` services are not running"
		s.Fix = "container system start"
		return []Result{r, s}
	}
	s.Status, s.Message = OK, "services running"
	rs := []Result{r, s}
	if b, ok := d.containerBuild(ctx, string(status)); ok {
		rs = append(rs, b)
	}
	return rs
}

// runtimePlugins are where the Linux runtime plugin lives under
// `paths.installRoot`: Homebrew's layout, then Apple's pkg.
var runtimePlugins = []string{
	"libexec/container-plugins/container-runtime-linux/bin/container-runtime-linux",
	"libexec/container/plugins/container-runtime-linux/bin/container-runtime-linux",
}

// containerBuild reports how the container runtime plugin is signed. macOS
// keys Local Network rules by binary, and Homebrew's ad-hoc-signed builds
// get a fresh rule per version. kona reaches nodes at their IPs and
// publishes no ports, so it doesn't need that rule.
func (d *Doctor) containerBuild(ctx context.Context, status string) (Result, bool) {
	var root string
	for _, line := range strings.Split(status, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "paths.installRoot" {
			root = f[1]
		}
	}
	if root == "" {
		return Result{}, false
	}
	for _, rel := range runtimePlugins {
		path := filepath.Join(root, rel)
		if _, err := d.Stat(path); err != nil {
			continue
		}
		r := Result{Name: "container-build", Status: OK}
		// codesign prints the signature details on stderr.
		out, _ := d.Runner.Run(ctx, "sh", "-c", `codesign -dv --verbose=2 "$1" 2>&1`, "sh", path)
		team, adhoc := "", false
		for _, line := range strings.Split(string(out), "\n") {
			if v, ok := strings.CutPrefix(line, "TeamIdentifier="); ok && v != "not set" {
				team = v
			}
			adhoc = adhoc || line == "Signature=adhoc"
		}
		switch {
		case team != "":
			r.Message = "runtime signed by team " + team
		case adhoc:
			r.Message = "ad-hoc signed (Homebrew build): its macOS Local Network rule is per version; " +
				"kona publishes no ports, so it doesn't depend on it (docs/failure-modes.md)"
		default:
			r.Message = "runtime signature unknown: " + path
		}
		return r, true
	}
	return Result{}, false
}

func (d *Doctor) kernel(path string) Result {
	r := Result{Name: "kernel"}
	fix := "make -C kernel && mkdir -p \"$(dirname '" + path + "')\" && cp kernel/out/vmlinux-" +
		version.Get("KERNEL_VERSION") + "-kona '" + path + "'  (or pass --kernel)"
	if _, err := d.Stat(path); err != nil {
		r.Status = Fail
		r.Message = "kona guest kernel not found at " + path
		r.Fix = fix
		return r
	}
	// A stale kernel from an older kona would silently lack newer options
	// (e.g. CONFIG_INET_DIAG for Cilium), so the exact build is pinned.
	if want := version.Checksum("KONA_KERNEL_SHA256_ARM64"); want != "" && d.Hash != nil {
		got, err := d.Hash(path)
		if err != nil {
			r.Status, r.Message = Fail, "cannot read kernel: "+err.Error()
			return r
		}
		if got != want {
			r.Status = Fail
			r.Message = fmt.Sprintf("kernel at %s is not the build this kona expects (sha256 %.12s…, want %.12s…)", path, got, want)
			r.Fix = fix
			return r
		}
	}
	r.Status, r.Message = OK, path
	return r
}

// FileSHA256 returns the hex sha256 of a file.
func FileSHA256(path string) (string, error) {
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

func (d *Doctor) dns(ctx context.Context) Result {
	r := Result{Name: "dns"}
	ns, err := HostDNS(ctx, d.Runner)
	if err != nil || ns == "" {
		r.Status, r.Message = Warn, "no host DNS resolver found; nodes may not resolve names"
		r.Fix = "pass --dns <server> to kona create cluster"
		return r
	}
	r.Status, r.Message = OK, "nodes will use "+ns+" (the vmnet gateway forwarder is not relied on)"
	return r
}

// HostDNS returns the host's primary DNS resolver.
func HostDNS(ctx context.Context, r execx.Runner) (string, error) {
	out, err := r.Run(ctx, "scutil", "--dns")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver[0]") {
			if _, v, ok := strings.Cut(line, ":"); ok {
				return strings.TrimSpace(v), nil
			}
		}
	}
	return "", nil
}

// compare compares dotted numeric versions; missing parts count as 0.
func compare(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
