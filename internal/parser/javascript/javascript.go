package javascript

import (
	"regexp"
	"strings"

	"github.com/PeacexF/EnvGraph/internal/parser"
)

// process.env.FOO.
const name = `([A-Za-z_$][A-Za-z0-9_$]*)`

// string literal
const quoted = `(?:"([^"]+)"|'([^']+)'|` + "`([^`]+)`" + `)`

// objects that expose the environment
const roots = `(?:process\.env|import\.meta\.env)`

var (
	dotAccess   = regexp.MustCompile(roots + `\??\.` + name)
	indexAccess = regexp.MustCompile(roots + `\s*\[\s*` + quoted)

	// const { PORT, DB_HOST: host } = process.env
	destructure = regexp.MustCompile(`\{([^{}]*)\}\s*=\s*` + roots)

	// A destructured entry may be renamed or given a default; the name thatm atters is always the one on the left.
	destructured = regexp.MustCompile(`^\s*([A-Za-z_$][A-Za-z0-9_$]*)`)
)

// Parse reports the variables a JavaScript or TypeScript file reads
func Parse(filePath string, content []byte) (parser.Result, error) {
	var res parser.Result

	withStrings, without := sanitize(string(content))
	indexLines := strings.Split(withStrings, "\n")

	for i, line := range strings.Split(without, "\n") {
		if !strings.Contains(line, "env") && !strings.Contains(indexLines[i], "env") {
			continue
		}

		loc := parser.Location{File: filePath, Line: i + 1}

		// One call site can match several patterns; report it once per line.
		seen := make(map[string]bool)
		add := func(n string) {
			if n == "" || seen[n] {
				return
			}
			seen[n] = true
			res.Occurrences = append(res.Occurrences, parser.Occurrence{
				Name:     n,
				Kind:     parser.KindConsumption,
				Location: loc,
			})
		}

		for _, m := range dotAccess.FindAllStringSubmatch(line, -1) {
			add(m[1])
		}
		for _, m := range indexAccess.FindAllStringSubmatch(indexLines[i], -1) {
			add(firstNonEmpty(m[1:]))
		}
		for _, m := range destructure.FindAllStringSubmatch(line, -1) {
			for _, entry := range strings.Split(m[1], ",") {
				if got := destructured.FindStringSubmatch(entry); got != nil {
					add(got[1])
				}
			}
		}
	}

	return res, nil
}

func firstNonEmpty(groups []string) string {
	for _, g := range groups {
		if g != "" {
			return g
		}
	}
	return ""
}

// sanitize returns two views of the source with byte offsets and newlines preserved, so line numbers stay correct.
func sanitize(src string) (noComments, noStrings string) {
	withStrings := []byte(src)
	without := []byte(src)

	const (
		code = iota
		lineComment
		blockComment
		single
		double
		template
	)

	state := code

	// parsing has to drop back out of the string
	var depths []int

	blank := func(i int, alsoStrings bool) {
		if src[i] == '\n' {
			return
		}
		if alsoStrings {
			withStrings[i] = ' '
		}
		without[i] = ' '
	}

	for i := 0; i < len(src); i++ {
		c := src[i]

		switch state {
		case code:
			switch {
			case c == '/' && i+1 < len(src) && src[i+1] == '/':
				state = lineComment
				blank(i, true)
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				state = blockComment
				blank(i, true)
			case c == '\'':
				state = single
			case c == '"':
				state = double
			case c == '`':
				state = template
			case c == '{' && len(depths) > 0:
				depths[len(depths)-1]++
			case c == '}' && len(depths) > 0:
				if depths[len(depths)-1] == 0 {
					depths = depths[:len(depths)-1]
					state = template
				} else {
					depths[len(depths)-1]--
				}
			}

		case lineComment:
			if c == '\n' {
				state = code
			} else {
				blank(i, true)
			}

		case blockComment:
			blank(i, true)
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				i++
				blank(i, true)
				state = code
			}

		case single, double, template:
			if c == '\\' {
				blank(i, false)
				if i+1 < len(src) {
					i++
					blank(i, false)
				}
				continue
			}
			if state == template && c == '$' && i+1 < len(src) && src[i+1] == '{' {
				i++
				depths = append(depths, 0)
				state = code
				continue
			}
			if (state == single && c == '\'') ||
				(state == double && c == '"') ||
				(state == template && c == '`') {
				state = code
				continue
			}
			blank(i, false)
		}
	}

	return string(withStrings), string(without)
}
