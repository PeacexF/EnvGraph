package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
)

func newExplainCmd() *cobra.Command {
	var (
		flags      scanFlags
		showValues bool
	)

	cmd := &cobra.Command{
		Use:   "explain <variable> [path]",
		Short: "Trace one variable from source to consumer",
		Long: "Explain answers the question this tool exists for: where does this\n" +
			"variable come from, what does it pass through, and who reads it.\n\n" +
			"It reports a variable even when an ignore rule hides it from scan\n" +
			"and check, since asking about it by name is a deliberate act.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			res, report, cfg, err := flags.analyze(cmd, defaultPath(args[1:]))
			if err != nil {
				return err
			}

			v, ok := report.Find(name)
			if !ok {
				writeNotFound(cmd.OutOrStdout(), name, report)
				return failure{code: 1}
			}

			ignored := cfg.IgnoresVariable(name)
			writeExplanation(cmd.OutOrStdout(), v, len(res.Files), ignored, showValues)
			return nil
		},
	}

	flags.register(cmd)
	cmd.Flags().BoolVar(&showValues, "show-values", false,
		"print the value assigned to the variable (these are often secrets)")

	return cmd
}

// writeNotFound reports an unknown name, with near misses when there are any.
// A wrong-case or partial name is the common mistake and worth catching.
func writeNotFound(w io.Writer, name string, report *analyzer.Report) {
	fmt.Fprintf(w, "%sNo variable named %s in this project.%s\n", red, name, reset)

	if near := suggestions(name, report); len(near) > 0 {
		fmt.Fprintf(w, "\nDid you mean:\n")
		for _, s := range near {
			fmt.Fprintf(w, "  %s\n", s)
		}
	}
}

// suggestions finds names that differ only by case or contain the query.
func suggestions(name string, report *analyzer.Report) []string {
	needle := strings.ToLower(name)

	var out []string
	for _, v := range report.Variables {
		candidate := strings.ToLower(v.Name)
		if candidate == needle || strings.Contains(candidate, needle) || strings.Contains(needle, candidate) {
			out = append(out, v.Name)
		}
	}

	sort.Strings(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// writeExplanation prints the trace for one variable.
func writeExplanation(w io.Writer, v analyzer.Variable, files int, ignored, showValues bool) {
	glyph := ""
	switch v.Status {
	case analyzer.StatusMissing:
		glyph = "!"
	case analyzer.StatusUnused:
		glyph = "?"
	}

	fmt.Fprintf(w, "%s%s%s  %s%s%s%s\n\n",
		bold, v.Name, reset, statusColor(v.Status), glyph, v.Status, reset)

	if ignored {
		fmt.Fprintf(w, "%sIgnored by your configuration, so scan and check leave it out.%s\n\n",
			dim, reset)
	}

	writeSection(w, "from", sourceLines(v, showValues), red, "nothing supplies this")
	writeSection(w, "into", injectionLines(v), "", "no container or job receives it")
	writeSection(w, "read", consumerLines(v), yellow, "nothing reads it")

	writeAdvice(w, v, files)
}

// writeSection prints one labelled block, or the empty note when there is nothing to show.
func writeSection(w io.Writer, label string, lines []string, emptyColor, empty string) {
	if len(lines) == 0 {
		fmt.Fprintf(w, "%-6s %s%s%s\n\n", label, emptyColor, empty, reset)
		return
	}

	for i, line := range lines {
		if i == 0 {
			fmt.Fprintf(w, "%-6s %s\n", label, line)
			continue
		}
		fmt.Fprintf(w, "%-6s %s\n", "", line)
	}
	fmt.Fprintln(w)
}

func sourceLines(v analyzer.Variable, showValues bool) []string {
	out := make([]string, 0, len(v.Sources))

	for _, src := range v.Sources {
		line := location(src.Location)
		switch {
		case len(src.DerivedFrom) > 0:
			line += fmt.Sprintf("  %sderived from %s%s",
				dim, strings.Join(src.DerivedFrom, ", "), reset)
		case src.Origin != "":
			line += fmt.Sprintf("  %s%s%s", dim, originLabel(src.Origin), reset)
		case src.FromDefault:
			line += fmt.Sprintf("  %sfallback%s", dim, reset)
		case showValues && src.Value != "":
			line += fmt.Sprintf("  %s= %s%s", dim, src.Value, reset)
		}
		out = append(out, line)
	}

	return out
}

func injectionLines(v analyzer.Variable) []string {
	out := make([]string, 0, len(v.PassedTo))
	for _, inj := range v.PassedTo {
		out = append(out, fmt.Sprintf("%s %s(%s)%s",
			inj.Service, dim, location(inj.Location), reset))
	}
	return out
}

func consumerLines(v analyzer.Variable) []string {
	out := make([]string, 0, len(v.Consumers))
	for _, loc := range v.Consumers {
		out = append(out, location(loc))
	}
	return out
}

// writeAdvice says what to do about a variable that is not healthy.
func writeAdvice(w io.Writer, v analyzer.Variable, files int) {
	switch v.Status {
	case analyzer.StatusMissing:
		fmt.Fprintf(w, "%sNothing across %d scanned files provides %s. Add it to a .env\n"+
			"file, a compose environment block, a Dockerfile ENV, or a workflow\n"+
			"secret — or add it to `ignore` if it comes from somewhere EnvGraph\n"+
			"cannot see.%s\n", dim, files, v.Name, reset)

	case analyzer.StatusUnused:
		fmt.Fprintf(w, "%s%s is provided but nothing reads it. Either it is dead\n"+
			"configuration worth deleting, or it is read somewhere EnvGraph does\n"+
			"not parse yet.%s\n", dim, v.Name, reset)
	}
}
