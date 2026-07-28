package env_test

import (
	"strings"
	"testing"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/env"
)

type definition struct {
	Name  string
	Value string
	Line  int
}

func parse(t *testing.T, input string) parser.Result {
	t.Helper()

	res, err := env.Parse(".env", []byte(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return res
}

func assertDefinitions(t *testing.T, input string, want []definition) {
	t.Helper()

	res := parse(t, input)
	if len(res.Occurrences) != len(want) {
		t.Fatalf("got %d occurrences, want %d: %+v",
			len(res.Occurrences), len(want), res.Occurrences)
	}

	for i, w := range want {
		got := res.Occurrences[i]
		if got.Name != w.Name {
			t.Errorf("occurrence %d: name = %q, want %q", i, got.Name, w.Name)
		}
		if got.Value != w.Value {
			t.Errorf("occurrence %d: value = %q, want %q", i, got.Value, w.Value)
		}
		if got.Location.Line != w.Line {
			t.Errorf("occurrence %d: line = %d, want %d", i, got.Location.Line, w.Line)
		}
		if got.Kind != parser.KindDefinition {
			t.Errorf("occurrence %d: kind = %q, want %q", i, got.Kind, parser.KindDefinition)
		}
		if got.Location.File != ".env" {
			t.Errorf("occurrence %d: file = %q, want .env", i, got.Location.File)
		}
	}
}

func TestAssignments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []definition
	}{
		{
			name:  "simple",
			input: "DATABASE_URL=postgres://localhost\n",
			want:  []definition{{"DATABASE_URL", "postgres://localhost", 1}},
		},
		{
			name:  "comments and blank lines are skipped",
			input: "# a comment\n\nPORT=8080\n",
			want:  []definition{{"PORT", "8080", 3}},
		},
		{
			name:  "export prefix",
			input: "export TOKEN=abc\n",
			want:  []definition{{"TOKEN", "abc", 1}},
		},
		{
			name:  "surrounding whitespace",
			input: "  KEY = value  \n",
			want:  []definition{{"KEY", "value", 1}},
		},
		{
			name:  "empty value is still a definition",
			input: "EMPTY=\n",
			want:  []definition{{"EMPTY", "", 1}},
		},
		{
			name:  "value containing equals signs",
			input: "TOKEN=a=b=c\n",
			want:  []definition{{"TOKEN", "a=b=c", 1}},
		},
		{
			name:  "several variables",
			input: "A=1\nB=2\nC=3\n",
			want:  []definition{{"A", "1", 1}, {"B", "2", 2}, {"C", "3", 3}},
		},
		{
			name:  "windows line endings",
			input: "A=1\r\nB=2\r\n",
			want:  []definition{{"A", "1", 1}, {"B", "2", 2}},
		},
		{
			name:  "no trailing newline",
			input: "A=1",
			want:  []definition{{"A", "1", 1}},
		},
		{
			name:  "indented comment",
			input: "   # indented\nA=1\n",
			want:  []definition{{"A", "1", 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertDefinitions(t, tt.input, tt.want)
		})
	}
}

func TestQuoting(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []definition
	}{
		{
			name:  "double quotes are stripped",
			input: `MSG="hello world"` + "\n",
			want:  []definition{{"MSG", "hello world", 1}},
		},
		{
			name:  "single quotes are stripped",
			input: `MSG='hello world'` + "\n",
			want:  []definition{{"MSG", "hello world", 1}},
		},
		{
			name:  "escapes resolve inside double quotes",
			input: `MSG="line\nbreak\ttab"` + "\n",
			want:  []definition{{"MSG", "line\nbreak\ttab", 1}},
		},
		{
			name:  "single quotes keep backslashes literal",
			input: `PATTERN='a\nb'` + "\n",
			want:  []definition{{"PATTERN", `a\nb`, 1}},
		},
		{
			name:  "escaped quote inside double quotes",
			input: `MSG="say \"hi\""` + "\n",
			want:  []definition{{"MSG", `say "hi"`, 1}},
		},
		{
			name:  "quoted value keeps its hash",
			input: `KEY="a # b"` + "\n",
			want:  []definition{{"KEY", "a # b", 1}},
		},
		{
			name:  "empty quoted value",
			input: `KEY=""` + "\n",
			want:  []definition{{"KEY", "", 1}},
		},
		{
			name:  "multi-line quoted value",
			input: "KEY=\"first\nsecond\"\nNEXT=ok\n",
			want:  []definition{{"KEY", "first\nsecond", 1}, {"NEXT", "ok", 3}},
		},
		{
			name:  "unterminated quote takes the rest of the file",
			input: "KEY=\"never closed\nmore\n",
			want:  []definition{{"KEY", "never closed\nmore\n", 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertDefinitions(t, tt.input, tt.want)
		})
	}
}

func TestComments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []definition
	}{
		{
			name:  "trailing comment on unquoted value",
			input: "PORT=8080 # the http port\n",
			want:  []definition{{"PORT", "8080", 1}},
		},
		{
			name:  "hash inside a value is not a comment",
			input: "URL=http://host/page#section\n",
			want:  []definition{{"URL", "http://host/page#section", 1}},
		},
		{
			name:  "tab before the comment",
			input: "PORT=8080\t# note\n",
			want:  []definition{{"PORT", "8080", 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertDefinitions(t, tt.input, tt.want)
		})
	}
}

func TestIgnoredLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"no equals sign", "JUST_A_NAME\n"},
		{"name starting with a digit", "1BAD=x\n"},
		{"name with a dash", "BAD-NAME=x\n"},
		{"name with a dot", "BAD.NAME=x\n"},
		{"empty key", "=value\n"},
		{"only comments", "# one\n# two\n"},
		{"empty file", ""},
		{"only whitespace", "\n\n   \n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if res := parse(t, tt.input); len(res.Occurrences) != 0 {
				t.Errorf("Parse(%q) = %+v, want nothing", tt.input, res.Occurrences)
			}
		})
	}
}

func TestValidNamesAreAccepted(t *testing.T) {
	for _, name := range []string{"A", "_", "_A", "a", "A1", "A_B_C", "lowercase_ok"} {
		res := parse(t, name+"=x\n")
		if len(res.Occurrences) != 1 || res.Occurrences[0].Name != name {
			t.Errorf("name %q was not accepted: %+v", name, res.Occurrences)
		}
	}
}

func TestNoServicesReported(t *testing.T) {
	if res := parse(t, "A=1\n"); len(res.Services) != 0 {
		t.Errorf("services = %+v, want none: .env files declare no containers", res.Services)
	}
}

func TestLargeFile(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 1000; i++ {
		b.WriteString("VAR_")
		b.WriteString(strings.Repeat("X", 3))
		b.WriteString("=value\n")
	}

	if res := parse(t, b.String()); len(res.Occurrences) != 1000 {
		t.Errorf("got %d occurrences, want 1000", len(res.Occurrences))
	}
}

func TestEscapedClosingQuoteLeavesTheValueOpen(t *testing.T) {
	assertDefinitions(t, `KEY="the quote is escaped \"`+"\nNEXT=x\n",
		[]definition{{"KEY", "the quote is escaped \"\nNEXT=x\n", 1}})
}

func TestValueEndingInABackslash(t *testing.T) {
	assertDefinitions(t, `KEY="a\`, []definition{{"KEY", `a\`, 1}})
}

func TestUnknownEscapeKeepsTheCharacter(t *testing.T) {
	assertDefinitions(t, `KEY="a\qb"`+"\n", []definition{{"KEY", "aqb", 1}})
}
