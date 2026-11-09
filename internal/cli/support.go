package cli

import (
	"bytes"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/lestex/kona/internal/doctor"
	"github.com/lestex/kona/internal/execx"
	"github.com/lestex/kona/internal/version"
	"github.com/spf13/cobra"
)

func newSupportCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "support", Short: "Collect debugging information (bundle)"}
	var out string
	bundle := &cobra.Command{
		Use:   "bundle [NAME]",
		Short: "Write a .tar.gz with a cluster's state, checks, node logs and Kubernetes view (secrets redacted)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := DefaultClusterName
			if len(args) == 1 {
				name = args[0]
			}
			if out == "" {
				out = fmt.Sprintf("kona-%s-support-%s.tar.gz", name, time.Now().Format("20060102-150405"))
			}
			kernel, err := g.defaultKernel()
			if err != nil {
				return err
			}
			var doc bytes.Buffer
			printDoctor(&doc, doctor.New(execx.OS{}).Run(cmd.Context(), doctor.Options{Kernel: kernel}))
			extra := map[string][]byte{"version.txt": versionText(), "doctor.txt": doc.Bytes()}

			f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			if err := g.manager(cmd).SupportBundle(cmd.Context(), name, f, extra); err != nil {
				f.Close()
				os.Remove(out)
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Wrote %s (secrets redacted; review it before sharing)\n", out)
			return nil
		},
	}
	bundle.Flags().StringVarP(&out, "output", "o", "", "bundle path (default: kona-<name>-support-<time>.tar.gz)")
	cmd.AddCommand(bundle)
	return cmd
}

func versionText() []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "kona %s (commit %s)\n\n", version.Version, version.Commit)
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range version.Matrix() {
		fmt.Fprintf(w, "%s\t%s\n", c.Name, c.Version)
	}
	w.Flush()
	return b.Bytes()
}
