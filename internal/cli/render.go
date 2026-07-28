package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/workflow"
	"github.com/PeacexF/EnvGraph/internal/scanner"
)

var (
	bold   = ""
	dim    = ""
	red    = ""
	yellow = ""
	green  = ""
	reset  = ""
)

func init() {
	if !isTerminal(os.Stdout) || os.Getenv("NO_COLOR") != "" {
		return
	}
	bold, dim, red, yellow, green, reset =
		"\033[1m", "\033[2m", "\033[31m", "\033[33m", "\033[32m", "\033[0m"
}

func disableColor() {
	bold, dim, red, yellow, green, reset = "", "", "", "", "", ""
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// location renders "path:line", the form editors and terminals link on.
func location(loc parser.Location) string {
	return loc.File + ":" + strconv.Itoa(loc.Line)
}

func statusColor(s analyzer.Status) string {
	switch s {
	case analyzer.StatusMissing:
		return red
	case analyzer.StatusUnused:
		return yellow
	default:
		return green
	}
}

// writeReport prints every variable: where it comes from, where it is passed, and where it is read.
func writeReport(w io.Writer, res *scanner.Result, report *analyzer.Report, only map[analyzer.Status]bool, showValues bool) {
	fmt.Fprintf(w, "%sScanned%s %d files, found %d variables\n\n",
		bold, reset, len(res.Files), len(report.Variables))

	if len(report.Variables) == 0 {
		fmt.Fprintf(w, "%sNo environment variables found.%s\n", dim, reset)
		return
	}

	for _, v := range report.Variables {
		if only != nil && !only[v.Status] {
			continue
		}

		fmt.Fprintf(w, "%s%s%s  %s%s%s\n",
			bold, v.Name, reset, statusColor(v.Status), v.Status, reset)

		for _, src := range v.Sources {
			label := "source"
			switch {
			case src.FromDefault:
				label = "default"
			case len(src.DerivedFrom) > 0:
				label = "derived"
			case src.Origin != "":
				label = "external"
			}

			line := fmt.Sprintf("  %-10s %s", label, location(src.Location))
			switch {
			case len(src.DerivedFrom) > 0:
				line += fmt.Sprintf("  %sfrom %s%s",
					dim, strings.Join(src.DerivedFrom, ", "), reset)
			case src.Origin != "":
				line += fmt.Sprintf("  %s%s%s", dim, originLabel(src.Origin), reset)
			case showValues && src.Value != "":
				line += fmt.Sprintf("  %s= %s%s", dim, src.Value, reset)
			}
			fmt.Fprintln(w, line)
		}
		if len(v.Sources) == 0 {
			fmt.Fprintf(w, "  %-10s %s(none)%s\n", "source", red, reset)
		}

		for _, inj := range v.PassedTo {
			fmt.Fprintf(w, "  %-10s %s %s(%s)%s\n",
				"passed to", inj.Service, dim, location(inj.Location), reset)
		}

		for _, loc := range v.Consumers {
			fmt.Fprintf(w, "  %-10s %s\n", "used in", location(loc))
		}
		if len(v.Consumers) == 0 && len(v.PassedTo) == 0 {
			fmt.Fprintf(w, "  %-10s %s(nothing)%s\n", "used in", yellow, reset)
		}

		fmt.Fprintln(w)
	}

	writeSummary(w, report)
}

// originLabel names an external provider in words.
func originLabel(origin string) string {
	switch origin {
	case workflow.OriginSecret:
		return "GitHub secret"
	case workflow.OriginVar:
		return "GitHub repository variable"
	case workflow.OriginInput:
		return "workflow input"
	default:
		return origin
	}
}

func writeSummary(w io.Writer, report *analyzer.Report) {
	missing, unused := len(report.Missing()), len(report.Unused())
	ok := len(report.Variables) - missing - unused

	fmt.Fprintf(w, "%s%d ok%s", green, ok, reset)
	if missing > 0 {
		fmt.Fprintf(w, ", %s%d missing%s", red, missing, reset)
	}
	if unused > 0 {
		fmt.Fprintf(w, ", %s%d unused%s", yellow, unused, reset)
	}
	fmt.Fprintln(w)
}

// writeFindings prints the errors and warnings that `check` reports.
func writeFindings(w io.Writer, report *analyzer.Report) {
	for _, v := range report.Missing() {
		fmt.Fprintf(w, "%sERROR%s   %s is used but never provided\n", red, reset, v.Name)
		for _, loc := range v.Consumers {
			fmt.Fprintf(w, "        used in %s\n", location(loc))
		}
		for _, inj := range v.PassedTo {
			fmt.Fprintf(w, "        passed to %s at %s\n", inj.Service, location(inj.Location))
		}
		fmt.Fprintln(w)
	}

	for _, v := range report.Unused() {
		fmt.Fprintf(w, "%sWARNING%s %s is defined but never used\n", yellow, reset, v.Name)
		for _, src := range v.Sources {
			fmt.Fprintf(w, "        defined in %s\n", location(src.Location))
		}
		fmt.Fprintln(w)
	}
}
