package python

import (
	"regexp"
	"strings"

	"github.com/PeacexF/EnvGraph/internal/parser"
)

// Go's regexp has no backreferences, so both quote styles are spelled out and the name lands in whichever group matched.
const quoted = `(?:"([^"]+)"|'([^']+)')`

// No Python AST parser is available to Go, so this is a text match over comment-stripped lines.
var patterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:os\.)?(?:getenv|environ\.get)\s*\(\s*` + quoted),
	regexp.MustCompile(`\b(?:os\.)?environ\s*\[\s*` + quoted + `\s*\]`),
	regexp.MustCompile(`\b(?:os\.)?environ\.setdefault\s*\(\s*` + quoted),
}

// Parse reports the variables a Python file looks up.
func Parse(filePath string, content []byte) (parser.Result, error) {
	var res parser.Result

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	for i, raw := range lines {
		line := stripComment(raw)
		if line == "" {
			continue
		}

		// Two patterns can match the same call.
		seen := make(map[string]bool)

		for _, re := range patterns {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				name := m[1]
				if name == "" {
					name = m[2]
				}
				if seen[name] {
					continue
				}
				seen[name] = true

				res.Occurrences = append(res.Occurrences, parser.Occurrence{
					Name:     name,
					Kind:     parser.KindConsumption,
					Location: parser.Location{File: filePath, Line: i + 1},
				})
			}
		}
	}

	return res, nil
}

// stripComment removes a trailing "#" comment, ignoring hashes inside quotes.
func stripComment(line string) string {
	var quote byte

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#':
			return line[:i]
		}
	}

	return line
}
