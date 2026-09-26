package container

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lestex/kona/internal/execx"
)

func TestRunArgs(t *testing.T) {
	got := strings.Join(RunArgs(RunSpec{
		Name: "kona-worker-1", Image: "img:1", Kernel: "/k", Network: "kona-kona",
		MAC: "52:54:00:00:00:01", MTU: 1500, DNS: "8.8.8.8", CPUs: 2, Memory: "2G",
		Env:     map[string]string{"B": "2", "A": "1"},
		Labels:  map[string]string{LabelCluster: "kona"},
		Volumes: map[string]string{"db": "/var/lib/db"},
		Publish: []string{"51820:51820/udp"},
		Args:    []string{"agent", "--x"},
	}), " ")
	want := "run -d --progress none --name kona-worker-1 --init --cap-add ALL --masked-path NONE --read-only-path NONE " +
		"--kernel /k --network kona-kona,mac=52:54:00:00:00:01,mtu=1500 --dns 8.8.8.8 --cpus 2 --memory 2G " +
		"-e A=1 -e B=2 --label kona.cluster=kona -v db:/var/lib/db -p 51820:51820/udp img:1 agent --x"
	if got != want {
		t.Fatalf("RunArgs:\n got %s\nwant %s", got, want)
	}
}

func TestDeleteIgnoresNotFound(t *testing.T) {
	f := &execx.Fake{Responses: map[string]execx.FakeResponse{
		"container rm -f gone":      {Err: &execx.Error{Stderr: `Error: internalError: "failed to delete container" (cause: "notFound: ...")`, Err: errors.New("exit 1")}},
		"container network rm gone": {Err: &execx.Error{Stderr: `Error: failed to delete one or more networks: ["gone"]`, Err: errors.New("exit 1")}},
		"container rm -f boom":      {Err: &execx.Error{Stderr: "Error: xpc timeout", Err: errors.New("exit 1")}},
	}}
	c := New(f)
	ctx := context.Background()
	if err := c.Delete(ctx, "gone"); err != nil {
		t.Fatalf("Delete(gone) = %v", err)
	}
	if err := c.DeleteNetwork(ctx, "gone"); err != nil {
		t.Fatalf("DeleteNetwork(gone) = %v", err)
	}
	if err := c.Delete(ctx, "boom"); err == nil {
		t.Fatal("Delete(boom) = nil, want error")
	}
}

func TestContainersParse(t *testing.T) {
	f := &execx.Fake{Responses: map[string]execx.FakeResponse{
		"container ls -a --format json": {Out: []byte(`[{"id":"kona-control-plane-1",
		  "configuration":{"labels":{"kona.cluster":"kona"},"resources":{"cpus":2,"memoryInBytes":2147483648}},
		  "status":{"state":"running","networks":[{"network":"kona-kona","ipv4Address":"192.168.70.2/24","macAddress":"52:54:00:00:00:01"}]}}]`)},
	}}
	cs, err := New(f).Containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Configuration.Labels[LabelCluster] != "kona" || cs[0].Status.State != "running" ||
		cs[0].Status.Networks[0].MACAddress != "52:54:00:00:00:01" {
		t.Fatalf("unexpected parse: %+v", cs)
	}
}
