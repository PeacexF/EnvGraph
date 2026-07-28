package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
)

func newScanCmd() *cobra.Command {
	var (
		flags      scanFlags
		format     string
		output     string
		showValues bool
		only       []string
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

			statuses, err := parseOnly(only)
			if err != nil {
				return err
			}

			switch format {
			case "text":
				writeReport(w, res, report, statuses, showValues)
				return nil
			case "json":
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(analyzer.NewDocument(res, report.Only(statuses), showValues))
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
	cmd.Flags().StringSliceVar(&only, "only", nil,
		"list only these statuses: ok, missing, unused (repeatable)")

	return cmd
}

// parseOnly turns the --only values into a status set. Nil means everything.
func parseOnly(values []string) (map[analyzer.Status]bool, error) {
	if len(values) == 0 {
		return nil, nil
	}

	out := make(map[analyzer.Status]bool, len(values))
	for _, value := range values {
		switch status := analyzer.Status(strings.ToLower(strings.TrimSpace(value))); status {
		case analyzer.StatusOK, analyzer.StatusMissing, analyzer.StatusUnused:
			out[status] = true
		default:
			return nil, fmt.Errorf("unknown status %q: use ok, missing, or unused", value)
		}
	}
	return out, nil
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
