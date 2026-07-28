// Package dockerfile parses Dockerfiles
package dockerfile

import (
	"strings"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/interpolate"
)

// Parse reads a Dockerfile
func Parse(filePath string, content []byte) (parser.Result, error) {
	var res parser.Result

	for _, ins := range instructions(string(content)) {
		verb, rest, ok := strings.Cut(ins.text, " ")
		if !ok {
			continue
		}

		loc := parser.Location{File: filePath, Line: ins.line}

		switch strings.ToUpper(verb) {
		case "ENV":
			res.Occurrences = append(res.Occurrences, env(rest, loc)...)
		case "ARG":
			res.Occurrences = append(res.Occurrences, arg(rest, loc)...)
		}
	}

	return res, nil
}

// env handles both "ENV KEY=value KEY2=value2" and the legacy "ENV KEY the rest of the line is the value".
func env(rest string, loc parser.Location) []parser.Occurrence {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}

	pairs := splitPairs(rest)
	if pairs == nil {
		// Legacy form: one key, and everything after it is the value.
		key, value, _ := strings.Cut(rest, " ")
		if !validName(key) {
			return nil
		}
		return define(key, strings.TrimSpace(value), loc)
	}

	var out []parser.Occurrence
	for _, p := range pairs {
		if validName(p.key) {
			out = append(out, define(p.key, p.value, loc)...)
		}
	}
	return out
}

// define records a variable the file supplies, plus anything its value reads.
func define(key, value string, loc parser.Location) []parser.Occurrence {
	out := []parser.Occurrence{{
		Name:     key,
		Kind:     parser.KindDefinition,
		Value:    unquote(value),
		Location: loc,
	}}

	// "ENV PATH=/opt/bin:$PATH" reads PATH on the way to setting it.
	for _, ref := range interpolate.Find(value) {
		if ref.Name == key {
			continue
		}
		out = append(out, parser.Occurrence{
			Name:       ref.Name,
			Kind:       parser.KindReference,
			Location:   loc,
			HasDefault: ref.HasDefault,
		})
	}

	return out
}

// arg handles "ARG KEY" and "ARG KEY=default".
func arg(rest string, loc parser.Location) []parser.Occurrence {
	rest = strings.TrimSpace(rest)
	key, value, hasDefault := strings.Cut(rest, "=")
	key = strings.TrimSpace(key)

	if !validName(key) {
		return nil
	}

	if !hasDefault {
		return []parser.Occurrence{{
			Name:     key,
			Kind:     parser.KindReference,
			Location: loc,
		}}
	}

	return define(key, strings.TrimSpace(value), loc)
}

type pair struct{ key, value string }

// splitPairs reads "KEY=value KEY2=value2", honouring quotes around values
func splitPairs(s string) []pair {
	var pairs []pair

	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}

		start := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			return nil // no "=" on the first token: legacy form
		}

		key := s[start:i]
		i++ // skip "="

		value, next := readValue(s, i)
		i = next

		pairs = append(pairs, pair{key: key, value: value})
	}

	return pairs
}

// readValue reads a possibly quoted value and returns it with the index just past it.
func readValue(s string, i int) (string, int) {
	if i >= len(s) {
		return "", i
	}

	if quote := s[i]; quote == '"' || quote == '\'' {
		i++
		start := i
		for i < len(s) && s[i] != quote {
			if s[i] == '\\' {
				i++
			}
			i++
		}
		value := s[start:min(i, len(s))]
		if i < len(s) {
			i++ // closing quote
		}
		return value, i
	}

	start := i
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		i++
	}
	return s[start:i], i
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

type instruction struct {
	text string
	line int
}

// instructions joins continued lines and drops comments, keeping the line number the instruction started on.
func instructions(content string) []instruction {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")

	var out []instruction
	var current strings.Builder
	startLine := 0

	for i, raw := range lines {
		line := strings.TrimSpace(raw)

		// A comment inside a continuation is skipped, not appended.
		if strings.HasPrefix(line, "#") {
			continue
		}

		if current.Len() == 0 {
			if line == "" {
				continue
			}
			startLine = i + 1
		}

		if strings.HasSuffix(line, "\\") {
			current.WriteString(strings.TrimSuffix(line, "\\"))
			current.WriteByte(' ')
			continue
		}

		current.WriteString(line)
		if text := strings.TrimSpace(current.String()); text != "" {
			out = append(out, instruction{text: text, line: startLine})
		}
		current.Reset()
	}

	if text := strings.TrimSpace(current.String()); text != "" {
		out = append(out, instruction{text: text, line: startLine})
	}

	return out
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
