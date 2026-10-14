package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/lestex/kona/internal/doctor"
	"github.com/lestex/kona/internal/execx"
	"github.com/spf13/cobra"
)

// errDoctorFailed makes doctor exit non-zero after printing its report.
var errDoctorFailed = errors.New("host prerequisites are not met (see fixes above)")

func newDoctorCmd(g *globals) *cobra.Command {
	var gpu bool
	var kernel string
	var output string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check host prerequisites: macOS, arch, container, kernel, DNS, krunkit, vmnet-helper",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if kernel == "" {
				k, err := g.defaultKernel()
				if err != nil {
					return err
				}
				kernel = k
			}
			rs := doctor.New(execx.OS{}).Run(cmd.Context(), doctor.Options{GPU: gpu, Kernel: kernel})
			if done, err := printStructured(cmd.OutOrStdout(), output, rs); done {
				if err != nil {
					return err
				}
			} else {
				printDoctor(cmd.OutOrStdout(), rs)
			}
			if doctor.Failed(rs) {
				return errDoctorFailed
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&gpu, "gpu", false, "treat GPU prerequisites (krunkit, vmnet-helper) as required")
	cmd.Flags().StringVar(&kernel, "kernel", "", "kona guest kernel to check (default: the installed one)")
	addOutputFlag(cmd, &output)
	return cmd
}

func printDoctor(w io.Writer, rs []doctor.Result) {
	marks := map[doctor.Status]string{doctor.OK: "✓", doctor.Warn: "!", doctor.Fail: "✗"}
	for _, r := range rs {
		fmt.Fprintf(w, "%s %-16s %s\n", marks[r.Status], r.Name, r.Message)
		if r.Fix != "" && r.Status != doctor.OK {
			fmt.Fprintf(w, "  %-16s fix: %s\n", "", r.Fix)
		}
	}
}
