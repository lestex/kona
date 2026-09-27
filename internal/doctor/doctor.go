// Package doctor checks host prerequisites and explains how to fix them.
package doctor

import (
	"context"
	"fmt"
	"os"
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
	// GPU enables the krunkit/vmnet-helper checks as failures instead of
	// warnings.
	GPU bool
	// Kernel is the kona kernel path to verify ("" = skip).
	Kernel string
}

// Doctor runs checks with injectable host access.
type Doctor struct {
	Runner execx.Runner
	GOOS   string
	GOARCH string
	Stat   func(string) (os.FileInfo, error)
}

// New returns a Doctor for the real host.
func New(r execx.Runner) *Doctor {
	return &Doctor{Runner: r, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Stat: os.Stat}
}

// Run executes every check.
func (d *Doctor) Run(ctx context.Context, o Options) []Result {
	rs := []Result{d.macOS(ctx), d.arch()}
	rs = append(rs, d.container(ctx)...)
	if o.Kernel != "" {
		rs = append(rs, d.kernel(o.Kernel))
	}
	rs = append(rs, d.dns(ctx))
	rs = append(rs, d.krunkit(ctx, o.GPU), d.vmnetHelper(ctx, o.GPU))
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
			"`container` networks are not reachable between VMs and vmnet-helper needs root"
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
	if _, err := d.Runner.Run(ctx, "container", "system", "status"); err != nil {
		s.Status, s.Message = Fail, "`container` services are not running"
		s.Fix = "container system start"
	} else {
		s.Status, s.Message = OK, "services running"
	}
	return []Result{r, s}
}

func (d *Doctor) kernel(path string) Result {
	r := Result{Name: "kernel"}
	if _, err := d.Stat(path); err != nil {
		r.Status = Fail
		r.Message = "kona guest kernel not found at " + path
		r.Fix = "make -C kernel && mkdir -p \"$(dirname '" + path + "')\" && cp kernel/out/vmlinux-" +
			version.Get("KERNEL_VERSION") + "-kona '" + path + "'  (or pass --kernel)"
		return r
	}
	r.Status, r.Message = OK, path
	return r
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

func (d *Doctor) krunkit(ctx context.Context, gpu bool) Result {
	r := Result{Name: "krunkit"}
	missing := Warn
	if gpu {
		missing = Fail
	}
	fix := "brew tap libkrun/krun && brew trust --formula libkrun/krun/krunkit libkrun/krun/libkrun " +
		"libkrun/krun/libkrunfw libkrun/krun/virglrenderer-krun libkrun/krun/gvproxy && brew install libkrun/krun/krunkit"
	if out, err := d.Runner.Run(ctx, "brew", "list", "--full-name"); err == nil &&
		strings.Contains(string(out), "slp/krunkit/") {
		r.Status = missing
		r.Message = "krunkit is installed from the deprecated slp/krunkit tap"
		r.Fix = "brew list --full-name | grep '^slp/krunkit/' | xargs brew uninstall && brew untap slp/krunkit && " + fix
		return r
	}
	out, err := d.Runner.Run(ctx, "krunkit", "--version")
	if err != nil {
		r.Status, r.Message, r.Fix = missing, "krunkit not installed (needed for --gpu-workers)", fix
		return r
	}
	got := versionRE.FindString(string(out))
	if want := version.Get("KRUNKIT_VERSION"); want != "" && compare(got, want) < 0 {
		r.Status, r.Message, r.Fix = missing, fmt.Sprintf("krunkit %s is older than %s", got, want), "brew upgrade libkrun/krun/krunkit"
		return r
	}
	r.Status, r.Message = OK, "krunkit "+got
	return r
}

func (d *Doctor) vmnetHelper(ctx context.Context, gpu bool) Result {
	r := Result{Name: "vmnet-helper"}
	missing := Warn
	if gpu {
		missing = Fail
	}
	fix := "brew tap nirs/vmnet-helper && brew trust nirs/vmnet-helper && brew install vmnet-helper"
	prefix, err := d.Runner.Run(ctx, "brew", "--prefix", "vmnet-helper")
	if err != nil {
		r.Status, r.Message, r.Fix = missing, "vmnet-helper not installed (needed for --gpu-workers)", fix
		return r
	}
	bin := strings.TrimSpace(string(prefix)) + "/libexec/vmnet-helper"
	out, err := d.Runner.Run(ctx, bin, "--version")
	if err != nil {
		r.Status, r.Message, r.Fix = missing, "vmnet-helper not installed (needed for --gpu-workers)", fix
		return r
	}
	got := versionRE.FindString(string(out))
	if want := version.Get("VMNET_HELPER_VERSION"); want != "" && compare(got, want) < 0 {
		r.Status, r.Message, r.Fix = missing, fmt.Sprintf("vmnet-helper %s is older than %s", got, want), "brew upgrade vmnet-helper"
		return r
	}
	r.Status, r.Message = OK, "vmnet-helper "+got
	return r
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
