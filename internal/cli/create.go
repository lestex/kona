package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lestex/kona/internal/cluster"
	"github.com/lestex/kona/internal/doctor"
	"github.com/lestex/kona/internal/execx"
	"github.com/lestex/kona/internal/runtime/container"
	"github.com/spf13/cobra"
)

func newCreateCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "create", Short: "Create a resource (cluster)"}
	cmd.AddCommand(newCreateClusterCmd(g))
	return cmd
}

func newCreateClusterCmd(g *globals) *cobra.Command {
	o := cluster.CreateOptions{}
	cmd := &cobra.Command{
		Use:   "cluster [NAME]",
		Short: "Create a Kubernetes cluster: one Apple container VM per node",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			o.Name = DefaultClusterName
			if len(args) == 1 {
				o.Name = args[0]
			}
			if o.Kernel == "" {
				k, err := g.defaultKernel()
				if err != nil {
					return err
				}
				o.Kernel = k
			}
			run := execx.OS{}
			rs := doctor.New(run).Run(ctx, doctor.Options{GPU: o.GPUWorkers > 0, Kernel: o.Kernel})
			if doctor.Failed(rs) {
				printDoctor(cmd.ErrOrStderr(), rs)
				return errDoctorFailed
			}
			if o.DNS == "" {
				o.DNS, _ = doctor.HostDNS(ctx, run)
			}
			if o.Image == "" {
				o.Image = cluster.DefaultImage(o.Distro, o.K8sVersion)
			}
			o.HostMemory = hostMemory(ctx, run)
			m := cluster.NewManager(g.store, container.New(run), run, cmd.ErrOrStderr())
			return m.Create(ctx, o)
		},
	}
	f := cmd.Flags()
	f.IntVar(&o.Workers, "workers", 2, "number of worker nodes")
	f.IntVar(&o.ControlPlanes, "control-planes", 1, "number of control-plane nodes (odd; >1 uses embedded etcd)")
	f.IntVar(&o.GPUWorkers, "gpu-workers", 0, "number of krunkit GPU worker nodes")
	f.StringVar(&o.Distro, "distro", "k3s", "Kubernetes distribution: k3s|kubeadm")
	f.StringVar(&o.CNI, "cni", "default", "CNI: default|cilium")
	f.StringVar(&o.K8sVersion, "k8s-version", "", "Kubernetes version (default: the pinned one; must match a published node image)")
	f.StringVar(&o.Image, "image", "", "node image (default: ghcr.io/lestex/kona-node:<version>)")
	f.StringVar(&o.Kernel, "kernel", "", "guest kernel for Apple container nodes (default: the installed kona kernel)")
	f.StringVar(&o.DNS, "dns", "", "DNS server for nodes (default: the host's primary resolver)")
	f.IntVar(&o.CPUs, "cpus", 2, "vCPUs per node")
	f.StringVar(&o.Memory, "memory", "2G", "fixed memory per node (VMs do not balloon)")
	f.DurationVar(&o.Wait, "wait", 5*time.Minute, "how long to wait for all nodes to be Ready")
	f.BoolVar(&o.Retain, "retain", false, "on failure keep VMs and state for debugging/resuming instead of rolling back")
	return cmd
}

func hostMemory(ctx context.Context, r execx.Runner) int64 {
	out, err := r.Run(ctx, "sysctl", "-n", "hw.memsize")
	if err != nil {
		fmt.Fprintln(os.Stderr, "! cannot read host memory:", err)
		return 0
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return v
}
