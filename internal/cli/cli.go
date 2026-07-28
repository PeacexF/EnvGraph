package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
	"github.com/PeacexF/EnvGraph/internal/scanner"
)

// version is overridden at build time with -ldflags "-X ...cli.version=v1.2.3".
var version = "dev"

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	return Run(os.Args[1:], os.Stdout, os.Stderr)
}

// Run executes args against the given streams. Splitting this out of Execute is what makes the commands testable.
func Run(args []string, stdout, stderr io.Writer) int {
	if stdout != os.Stdout {
		disableColor()
	}

	root := &cobra.Command{
		Use:   "envgraph",
		Short: "Visualize how configuration flows through your application",
		Long: "EnvGraph analyzes project configuration and shows where environment\n" +
			"variables come from, where they are passed, and where they are used.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newScanCmd(), newCheckCmd(), newExportCmd(), newServeCmd())

	if err := root.Execute(); err != nil {
		// A failed check has already listed its findings.
		var fail failure
		if errors.As(err, &fail) {
			return fail.code
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	return 0
}

// failure carries an exit code out of a command without printing anything.
type failure struct{ code int }

func (f failure) Error() string { return fmt.Sprintf("exit status %d", f.code) }

// scanFlags are the options every command shares, since every command starts by scanning a project.
type scanFlags struct {
	exclude      []string
	includeTests bool
}

func (f *scanFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&f.exclude, "exclude", nil,
		"additional directory names to skip (repeatable)")
	cmd.Flags().BoolVar(&f.includeTests, "include-tests", false,
		"include test files when looking for variable usage")
}

// run scans and analyzes. Parse failures are reported but do not stop the run, so one malformed file cannot hide the rest of a project.
func (f *scanFlags) run(cmd *cobra.Command, path string) (*scanner.Result, *analyzer.Report, error) {
	res, err := scanner.Scan(path, scanner.Options{
		Exclude:      f.exclude,
		IncludeTests: f.includeTests,
	})
	if err != nil {
		return nil, nil, err
	}

	for _, e := range res.Errors {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", e)
	}

	return res, analyzer.Analyze(res), nil
}

func defaultPath(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}
