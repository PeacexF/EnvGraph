package interpolate

import "strings"

// Reference is one $VAR or ${VAR...} mention.
type Reference struct {
	Name string

	// HasDefault covers ${VAR:-x} and ${VAR-x}, which always yield a value.
	HasDefault bool
}

// Find extracts the references in a value. "$$" is a literal "$".
func Find(s string) []Reference {
	var refs []Reference

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
				refs = append(refs, Reference{Name: name})
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

		ref := Reference{Name: name}
		if op := inner[n:]; strings.HasPrefix(op, ":-") || strings.HasPrefix(op, "-") {
			ref.HasDefault = true
		}
		refs = append(refs, ref)

		// The default may interpolate too: ${HOST:-${FALLBACK}}.
		if n < len(inner) {
			refs = append(refs, Find(inner[n:])...)
		}
	}

	return refs
}

// Unresolved returns the names that must come from elsewhere, dropping any reference that carries its own fallback.
func Unresolved(refs []Reference) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if !ref.HasDefault {
			out = append(out, ref.Name)
		}
	}
	return out
}

// matchBrace takes a string starting with "{" and returns the index of the
// brace closing it, or -1.
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
