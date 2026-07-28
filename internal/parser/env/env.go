package env

import (
	"strings"

	"github.com/PeacexF/EnvGraph/internal/parser"
)

// Parse reads a .env file. Every assignment is a definition, including KEY=, since the empty string is still a value the process will see.
func Parse(path string, content []byte) (parser.Result, error) {
	var res parser.Result

	lines := splitLines(string(content))
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		lineNo := i + 1
		line = strings.TrimPrefix(line, "export ")

		key, rest, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if !validName(key) {
			continue
		}

		value, consumed := readValue(rest, lines[i+1:])
		i += consumed

		res.Occurrences = append(res.Occurrences, parser.Occurrence{
			Name:     key,
			Kind:     parser.KindDefinition,
			Value:    value,
			Location: parser.Location{File: path, Line: lineNo},
		})
	}

	return res, nil
}

// readValue returns the value after "=", plus the number of extra lines an unterminated quote consumed.
func readValue(rest string, following []string) (string, int) {
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		return "", 0
	}

	quote := rest[0]
	if quote != '"' && quote != '\'' {
		return trimComment(rest), 0
	}

	body := rest[1:]
	if end := closingQuote(body, quote); end >= 0 {
		return unescape(body[:end], quote), 0
	}

	// Keys and certificates are routinely wrapped across lines.
	var b strings.Builder
	b.WriteString(body)
	for n, next := range following {
		b.WriteByte('\n')
		if end := closingQuote(next, quote); end >= 0 {
			b.WriteString(next[:end])
			return unescape(b.String(), quote), n + 1
		}
		b.WriteString(next)
	}

	// Never closed: take what we have rather than dropping the variable.
	return unescape(b.String(), quote), len(following)
}

func closingQuote(s string, quote byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && quote == '"' {
			i++
			continue
		}
		if s[i] == quote {
			return i
		}
	}
	return -1
}

// unescape resolves backslash escapes, which only apply to double quotes.
func unescape(s string, quote byte) string {
	if quote != '"' || !strings.Contains(s, `\`) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// trimComment drops a trailing comment. A "#" only starts one when preceded by whitespace, so URL fragments survive.
func trimComment(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != '#' {
			continue
		}
		if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
			return strings.TrimRight(s[:i], " \t")
		}
	}
	return strings.TrimRight(s, " \t")
}

func validName(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}
