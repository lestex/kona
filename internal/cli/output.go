package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// addOutputFlag registers -o/--output on a get-style command.
func addOutputFlag(cmd *cobra.Command, out *string) {
	cmd.Flags().StringVarP(out, "output", "o", "", "output format: json|yaml (default: table)")
}

// printStructured writes v as JSON or YAML. It returns false for the table
// format so callers can print their own table.
func printStructured(w io.Writer, format string, v any) (bool, error) {
	switch format {
	case "":
		return false, nil
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return true, enc.Encode(v)
	case "yaml":
		b, err := yaml.Marshal(v)
		if err != nil {
			return true, err
		}
		_, err = w.Write(b)
		return true, err
	default:
		return true, fmt.Errorf("unknown output format %q (want json or yaml)", format)
	}
}

func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
}
