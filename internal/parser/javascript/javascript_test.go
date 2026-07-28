package javascript_test

import (
	"testing"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/javascript"
)

func names(t *testing.T, src string) []string {
	t.Helper()

	res, err := javascript.Parse("app.js", []byte(src))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	out := make([]string, 0, len(res.Occurrences))
	for _, occ := range res.Occurrences {
		if occ.Kind != parser.KindConsumption {
			t.Errorf("occurrence %+v: kind = %q, want %q", occ, occ.Kind, parser.KindConsumption)
		}
		out = append(out, occ.Name)
	}
	return out
}

func assertNames(t *testing.T, src string, want []string) {
	t.Helper()

	got := names(t, src)
	if len(got) != len(want) {
		t.Fatalf("Parse(%q) = %v, want %v", src, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("name %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDotAccess(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"simple", `const url = process.env.DATABASE_URL`, []string{"DATABASE_URL"}},
		{"with a fallback", `const port = process.env.PORT || 3000`, []string{"PORT"}},
		{"in a template literal expression", "const s = `port ${process.env.PORT}`", []string{"PORT"}},
		{"two on one line", `const a = process.env.A, b = process.env.B`, []string{"A", "B"}},
		{"vite import.meta.env", `const api = import.meta.env.VITE_API_URL`, []string{"VITE_API_URL"}},
		{"typescript non-null assertion", `const url = process.env.DB_URL!`, []string{"DB_URL"}},
		{"optional chaining", `const x = process.env?.MAYBE`, []string{"MAYBE"}},
		{"underscore name", `process.env._PRIVATE`, []string{"_PRIVATE"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNames(t, tt.src, tt.want)
		})
	}
}

func TestIndexAccess(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"double quotes", `const url = process.env["DATABASE_URL"]`, []string{"DATABASE_URL"}},
		{"single quotes", `const url = process.env['DATABASE_URL']`, []string{"DATABASE_URL"}},
		{"backticks", "const url = process.env[`DATABASE_URL`]", []string{"DATABASE_URL"}},
		{"whitespace inside", `process.env[ "SPACED" ]`, []string{"SPACED"}},
		{"vite index", `import.meta.env["VITE_KEY"]`, []string{"VITE_KEY"}},
		{"computed name", `process.env[key]`, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNames(t, tt.src, tt.want)
		})
	}
}

func TestDestructuring(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"const", `const { PORT, DB_HOST } = process.env`, []string{"PORT", "DB_HOST"}},
		{"let", `let { PORT } = process.env`, []string{"PORT"}},
		{"renamed", `const { DB_HOST: host } = process.env`, []string{"DB_HOST"}},
		{"with a default", `const { PORT = 3000 } = process.env`, []string{"PORT"}},
		{"renamed with a default", `const { PORT: p = 3000 } = process.env`, []string{"PORT"}},
		{"several with mixed forms", `const { A, B: b, C = 1 } = process.env`, []string{"A", "B", "C"}},
		{"no spaces", `const {A,B}=process.env`, []string{"A", "B"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNames(t, tt.src, tt.want)
		})
	}
}

func TestCommentsAreIgnored(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"line comment", `// process.env.COMMENTED`, nil},
		{"trailing line comment", "const a = process.env.REAL // process.env.NOPE", []string{"REAL"}},
		{"block comment", `/* process.env.COMMENTED */`, nil},
		{"multi-line block comment", "/*\nprocess.env.A\nprocess.env.B\n*/\nprocess.env.REAL", []string{"REAL"}},
		{"jsdoc", "/** reads process.env.DOCUMENTED */\nprocess.env.REAL", []string{"REAL"}},
		{"a url is not a comment", `const u = "http://example.com"; process.env.REAL`, []string{"REAL"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNames(t, tt.src, tt.want)
		})
	}
}

func TestStringContentsAreNotDotAccesses(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"in a log message", `console.log("set process.env.MENTIONED first")`, nil},
		{"in single quotes", `throw new Error('process.env.MENTIONED is unset')`, nil},
		{"in a template literal", "console.log(`process.env.MENTIONED`)", nil},
		{"message plus a real read", `console.log("use process.env.DOCS", process.env.REAL)`, []string{"REAL"}},
		{"escaped quote inside a string", `const s = "a \" process.env.NOPE"; process.env.REAL`, []string{"REAL"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNames(t, tt.src, tt.want)
		})
	}
}

func TestSameNameTwiceOnALineIsReportedOnce(t *testing.T) {
	assertNames(t, `const a = process.env.SAME || process.env.SAME`, []string{"SAME"})
}

func TestSameNameOnDifferentLinesIsReportedTwice(t *testing.T) {
	assertNames(t, "process.env.SAME\nprocess.env.SAME", []string{"SAME", "SAME"})
}

func TestLocations(t *testing.T) {
	res, err := javascript.Parse("src/config.ts", []byte("import x from 'y'\n\nexport const port = process.env.PORT\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(res.Occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1: %+v", len(res.Occurrences), res.Occurrences)
	}
	if got := res.Occurrences[0].Location.Line; got != 3 {
		t.Errorf("line = %d, want 3", got)
	}
	if got := res.Occurrences[0].Location.File; got != "src/config.ts" {
		t.Errorf("file = %q, want src/config.ts", got)
	}
}

func TestLineNumbersSurviveBlockComments(t *testing.T) {
	res, err := javascript.Parse("app.js", []byte("/*\n\n\n*/\nprocess.env.AFTER\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(res.Occurrences) != 1 || res.Occurrences[0].Location.Line != 5 {
		t.Errorf("occurrences = %+v, want AFTER on line 5", res.Occurrences)
	}
}

func TestUnrelatedCode(t *testing.T) {
	for _, src := range []string{
		`const env = "production"`,
		`config.env.SOMETHING`,
		`myProcess.env.SOMETHING`,
		`const { A } = someOtherObject`,
		``,
		`function main() { return 1 }`,
	} {
		if got := names(t, src); len(got) != 0 {
			t.Errorf("Parse(%q) = %v, want nothing", src, got)
		}
	}
}

func TestWindowsLineEndings(t *testing.T) {
	res, err := javascript.Parse("app.js", []byte("const a = 1\r\nprocess.env.PORT\r\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(res.Occurrences) != 1 || res.Occurrences[0].Location.Line != 2 {
		t.Errorf("occurrences = %+v, want PORT on line 2", res.Occurrences)
	}
}

func TestRealisticFile(t *testing.T) {
	assertNames(t, `import express from "express";

// Configuration is read once at startup.
const { NODE_ENV, PORT = 3000 } = process.env;

const config = {
  database: process.env.DATABASE_URL,
  redis: process.env["REDIS_URL"],
  /* secrets are injected by the platform */
  jwt: process.env.JWT_SECRET,
};

if (!config.jwt) {
  throw new Error("set process.env.JWT_SECRET before starting");
}
`, []string{"NODE_ENV", "PORT", "DATABASE_URL", "REDIS_URL", "JWT_SECRET"})
}

func TestNoServicesReported(t *testing.T) {
	res, err := javascript.Parse("app.js", []byte(`process.env.A`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(res.Services) != 0 {
		t.Errorf("services = %+v, want none", res.Services)
	}
}
