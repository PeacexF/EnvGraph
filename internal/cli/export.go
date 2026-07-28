package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
)

func newExportCmd() *cobra.Command {
	var (
		flags  scanFlags
		output string
	)

	cmd := &cobra.Command{
		Use:   "export [path]",
		Short: "Write the configuration graph as JSON",
		Long: "Export writes the raw node/edge graph, which is what the web\n" +
			"viewer reads and what other tools can consume.\n\n" +
			"Use `scan --format json` instead when you also want the per-variable\n" +
			"analysis alongside the graph.",
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

			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			return enc.Encode(analyzer.Graph(res, report))
		},
	}

	flags.register(cmd)
	cmd.Flags().StringVarP(&output, "output", "o", "graph.json",
		"file to write, or - for stdout")

	return cmd
}
