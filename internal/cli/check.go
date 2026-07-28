package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	var (
		flags  scanFlags
		strict bool
	)

	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Fail when configuration is missing",
		Long: "Check reports variables that are used but never provided, and\n" +
			"variables that are provided but never used.\n\n" +
			"It exits with status 1 when a variable is missing, which makes it\n" +
			"usable as a CI step. Unused variables are warnings unless --strict\n" +
			"is given.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, report, err := flags.run(cmd, defaultPath(args))
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			writeFindings(out, report)

			missing, unused := len(report.Missing()), len(report.Unused())
			switch {
			case missing == 0 && unused == 0:
				fmt.Fprintf(out, "%sAll %d variables are provided and used.%s\n",
					green, len(report.Variables), reset)
				return nil
			case missing > 0:
				fmt.Fprintf(out, "%d missing, %d unused\n", missing, unused)
				return failure{code: 1}
			case strict:
				fmt.Fprintf(out, "%d unused\n", unused)
				return failure{code: 1}
			default:
				fmt.Fprintf(out, "%d unused\n", unused)
				return nil
			}
		},
	}

	flags.register(cmd)
	cmd.Flags().BoolVar(&strict, "strict", false, "also fail when a variable is unused")

	return cmd
}
