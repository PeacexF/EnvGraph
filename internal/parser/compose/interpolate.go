package compose

import "strings"

type reference struct {
	name string

	// hasDefault covers ${VAR:-x} and ${VAR-x}, which always yield a value.
	hasDefault bool
}

// findReferences extracts $VAR and ${VAR...} mentions. "$$" is a literal "$".
func findReferences(s string) []reference {
	var refs []reference

	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			continue
		}
		if i+1 >= len(s) {
			break
		}

		if s[i+1] == '$' {
			i++
			continue
		}

		if s[i+1] != '{' {
			name, n := scanName(s[i+1:])
			if name != "" {
				refs = append(refs, reference{name: name})
			}
			i += n
			continue
		}

		end := matchBrace(s[i+1:])
		if end < 0 {
			break
		}
		inner := s[i+2 : i+1+end]
		i += 1 + end

		name, n := scanName(inner)
		if name == "" {
			continue
		}

		ref := reference{name: name}
		if op := inner[n:]; strings.HasPrefix(op, ":-") || strings.HasPrefix(op, "-") {
			ref.hasDefault = true
		}
		refs = append(refs, ref)

		// The default may interpolate too: ${HOST:-${FALLBACK}}.
		if n < len(inner) {
			refs = append(refs, findReferences(inner[n:])...)
		}
	}

	return refs
}

// matchBrace takes a string starting with "{" and returns the index of the brace closing it, or -1.
func matchBrace(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// scanName reads a leading shell variable name and returns it with its length.
func scanName(s string) (string, int) {
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return s[:i], i
		}
	}
	return s, i
}
