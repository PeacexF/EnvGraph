package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
	"github.com/PeacexF/EnvGraph/internal/config"
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
	root.AddCommand(newScanCmd(), newCheckCmd(), newExplainCmd(), newExportCmd(), newServeCmd())

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
	ignore       []string
	configPath   string
	noConfig     bool
}

func (f *scanFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&f.exclude, "exclude", nil,
		"additional directory names to skip (repeatable)")
	cmd.Flags().BoolVar(&f.includeTests, "include-tests", false,
		"include test files when looking for variable usage")
	cmd.Flags().StringSliceVar(&f.ignore, "ignore", nil,
		"variable names to drop, glob wildcards allowed (repeatable)")
	cmd.Flags().StringVar(&f.configPath, "config", "",
		"path to .envgraph.yml (defaults to one in the scanned directory)")
	cmd.Flags().BoolVar(&f.noConfig, "no-config", false,
		"ignore any .envgraph.yml, including the built-in system variables")
}

// config resolves the file plus the flags layered on top.
func (f *scanFlags) config(root string) (*config.Config, error) {
	if f.noConfig {
		off := false
		return (&config.Config{SystemVariables: &off}).WithIgnored(f.ignore...), nil
	}

	cfg, err := config.Load(root, f.configPath)
	if err != nil {
		return nil, err
	}
	return cfg.WithIgnored(f.ignore...), nil
}

// scanOptions merges the config's excludes with the flag's.
func (f *scanFlags) scanOptions(cfg *config.Config) scanner.Options {
	return scanner.Options{
		Exclude:      append(append([]string(nil), cfg.Exclude...), f.exclude...),
		IncludeTests: f.includeTests,
	}
}

// run scans and analyzes. Parse failures are reported but do not stop the run, so one malformed file cannot hide the rest of a project.
func (f *scanFlags) run(cmd *cobra.Command, path string) (*scanner.Result, *analyzer.Report, error) {
	res, full, cfg, err := f.analyze(cmd, path)
	if err != nil {
		return nil, nil, err
	}
	return res, full.Without(cfg.IgnoresVariable), nil
}

// analyze is run without the ignore rules applied, which `explain` needs so it can say "that variable is ignored" rather than "no such variable".
func (f *scanFlags) analyze(cmd *cobra.Command, path string) (*scanner.Result, *analyzer.Report, *config.Config, error) {
	cfg, err := f.config(path)
	if err != nil {
		return nil, nil, nil, err
	}

	res, err := scanner.Scan(path, f.scanOptions(cfg))
	if err != nil {
		return nil, nil, nil, err
	}

	for _, e := range res.Errors {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", e)
	}

	return res, analyzer.Analyze(res), cfg, nil
}

func defaultPath(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}
