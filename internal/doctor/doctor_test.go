package doctor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/lestex/kona/internal/execx"
)

func healthy() *execx.Fake {
	return &execx.Fake{Responses: map[string]execx.FakeResponse{
		"sw_vers -productVersion":    {Out: []byte("26.6.2\n")},
		"container --version":        {Out: []byte("container CLI version 1.4.1 (build: release)\n")},
		"scutil --dns":               {Out: []byte("resolver #1\n  nameserver[0] : 8.8.8.8\n")},
		"krunkit --version":          {Out: []byte("krunkit 1.3.2\n")},
		"brew --prefix vmnet-helper": {Out: []byte("/opt/homebrew/opt/vmnet-helper\n")},
		"/opt/homebrew/opt/vmnet-helper/libexec/vmnet-helper --version": {Out: []byte("version: v0.13.0\n")},
	}}
}

func newDoctor(f *execx.Fake) *Doctor {
	return &Doctor{Runner: f, GOOS: "darwin", GOARCH: "arm64",
		Stat: func(string) (os.FileInfo, error) { return nil, nil }}
}

func find(rs []Result, name string) Result {
	for _, r := range rs {
		if r.Name == name {
			return r
		}
	}
	return Result{}
}

func TestHealthyHost(t *testing.T) {
	rs := newDoctor(healthy()).Run(context.Background(), Options{GPU: true, Kernel: "/k"})
	if Failed(rs) {
		t.Fatalf("healthy host failed: %+v", rs)
	}
}

func TestMacOS15FailsWithReason(t *testing.T) {
	f := healthy()
	f.Responses["sw_vers -productVersion"] = execx.FakeResponse{Out: []byte("15.5\n")}
	r := find(newDoctor(f).Run(context.Background(), Options{}), "macos")
	if r.Status != Fail || !strings.Contains(r.Message, "macOS 26") {
		t.Fatalf("macos result = %+v", r)
	}
}

func TestMissingContainerAndKernel(t *testing.T) {
	f := healthy()
	f.Responses["container --version"] = execx.FakeResponse{Err: errors.New("not found")}
	d := newDoctor(f)
	d.Stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	rs := d.Run(context.Background(), Options{Kernel: "/missing"})
	if c := find(rs, "container"); c.Status != Fail || c.Fix != "brew install container && container system start" {
		t.Fatalf("container result = %+v", c)
	}
	if k := find(rs, "kernel"); k.Status != Fail || !strings.Contains(k.Fix, "make -C kernel") {
		t.Fatalf("kernel result = %+v", k)
	}
}

func TestGPUToolsOnlyFailWhenRequested(t *testing.T) {
	f := healthy()
	f.Responses["krunkit --version"] = execx.FakeResponse{Err: errors.New("not found")}
	if r := find(newDoctor(f).Run(context.Background(), Options{}), "krunkit"); r.Status != Warn {
		t.Fatalf("krunkit without --gpu = %+v, want warn", r)
	}
	if r := find(newDoctor(f).Run(context.Background(), Options{GPU: true}), "krunkit"); r.Status != Fail {
		t.Fatalf("krunkit with --gpu = %+v, want fail", r)
	}
}

func TestDeprecatedKrunkitTap(t *testing.T) {
	f := healthy()
	f.Responses["brew list --full-name"] = execx.FakeResponse{Out: []byte("slp/krunkit/krunkit\n")}
	r := find(newDoctor(f).Run(context.Background(), Options{GPU: true}), "krunkit")
	if r.Status != Fail || !strings.Contains(r.Fix, "brew untap slp/krunkit") {
		t.Fatalf("deprecated tap result = %+v", r)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"26.6.2", "26", 1}, {"15.5", "26", -1}, {"1.4.1", "1.4.1", 0}, {"1.10", "1.9", 1}}
	for _, c := range cases {
		if got := compare(c.a, c.b); got != c.want {
			t.Errorf("compare(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}
