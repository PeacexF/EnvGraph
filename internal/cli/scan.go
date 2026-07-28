package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
)

func newScanCmd() *cobra.Command {
	var (
		flags      scanFlags
		format     string
		output     string
		showValues bool
	)

	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Analyze a project and report its configuration flow",
		Long: "Scan walks a project, reads its .env files, compose files, and\n" +
			"source code, and reports where every environment variable comes\n" +
			"from and where it ends up.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, report, err := flags.run(cmd, defaultPath(args))
			if err != nil {
				return err
			}

			w, closeOut, err := openOutput(cmd.OutOrStdout(), output)
			if err != nil {
				return err
			}
			defer closeOut()

			switch format {
			case "text":
				writeReport(w, res, report, showValues)
				return nil
			case "json":
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(analyzer.NewDocument(res, report, showValues))
			default:
				return fmt.Errorf("unknown format %q: use text or json", format)
			}
		},
	}

	flags.register(cmd)
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text or json")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to a file instead of stdout")
	cmd.Flags().BoolVar(&showValues, "show-values", false,
		"print the value assigned to each variable (these are often secrets)")

	return cmd
}

// openOutput returns the destination and a close function. An empty path, or "-", means the command's own stdout, which must not be closed.
func openOutput(stdout io.Writer, path string) (io.Writer, func(), error) {
	if path == "" || path == "-" {
		return stdout, func() {}, nil
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("create %s: %w", path, err)
	}

	disableColor()

	return f, func() { f.Close() }, nil
}
