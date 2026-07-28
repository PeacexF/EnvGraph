package dockerfile_test

import (
	"testing"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/dockerfile"
)

func parse(t *testing.T, content string) parser.Result {
	t.Helper()

	res, err := dockerfile.Parse("Dockerfile", []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return res
}

func find(t *testing.T, res parser.Result, name string) parser.Occurrence {
	t.Helper()

	for _, occ := range res.Occurrences {
		if occ.Name == name {
			return occ
		}
	}
	t.Fatalf("no occurrence for %q in %+v", name, res.Occurrences)
	return parser.Occurrence{}
}

func names(res parser.Result) []string {
	out := make([]string, 0, len(res.Occurrences))
	for _, occ := range res.Occurrences {
		out = append(out, occ.Name)
	}
	return out
}

func assertDefinition(t *testing.T, res parser.Result, name, value string) {
	t.Helper()

	occ := find(t, res, name)
	if occ.Kind != parser.KindDefinition {
		t.Errorf("%s kind = %q, want %q", name, occ.Kind, parser.KindDefinition)
	}
	if occ.Value != value {
		t.Errorf("%s value = %q, want %q", name, occ.Value, value)
	}
}

func TestEnvKeyValue(t *testing.T) {
	res := parse(t, "FROM alpine\nENV DATABASE_URL=postgres://localhost\n")
	assertDefinition(t, res, "DATABASE_URL", "postgres://localhost")
}

func TestEnvSeveralPairsOnOneLine(t *testing.T) {
	res := parse(t, "ENV NODE_ENV=production PORT=3000 LOG_LEVEL=info\n")

	assertDefinition(t, res, "NODE_ENV", "production")
	assertDefinition(t, res, "PORT", "3000")
	assertDefinition(t, res, "LOG_LEVEL", "info")
}

func TestEnvQuotedValues(t *testing.T) {
	res := parse(t, `ENV GREETING="hello world" OTHER='single quoted'`+"\n")

	assertDefinition(t, res, "GREETING", "hello world")
	assertDefinition(t, res, "OTHER", "single quoted")
}

func TestEnvLegacyForm(t *testing.T) {
	// "ENV KEY value" takes the rest of the line as the value.
	res := parse(t, "ENV GREETING hello world\n")
	assertDefinition(t, res, "GREETING", "hello world")
}

func TestEnvEmptyValue(t *testing.T) {
	res := parse(t, "ENV EMPTY=\n")
	assertDefinition(t, res, "EMPTY", "")
}

func TestEnvReadsOtherVariables(t *testing.T) {
	res := parse(t, "ENV APP_HOME=/opt/app\nENV PATH=$APP_HOME/bin:$PATH\n")

	// PATH is set here, so it is a definition.
	if occ := find(t, res, "PATH"); occ.Kind != parser.KindDefinition {
		t.Errorf("PATH kind = %q, want %q", occ.Kind, parser.KindDefinition)
	}

	// APP_HOME is read on the way, which is a reference.
	var sawReference bool
	for _, occ := range res.Occurrences {
		if occ.Name == "APP_HOME" && occ.Kind == parser.KindReference {
			sawReference = true
		}
	}
	if !sawReference {
		t.Errorf("occurrences = %+v, want APP_HOME read by the PATH line", res.Occurrences)
	}
}

func TestEnvSelfReferenceIsNotDuplicated(t *testing.T) {
	res := parse(t, "ENV PATH=$PATH:/extra\n")

	if got := names(res); len(got) != 1 || got[0] != "PATH" {
		t.Errorf("names = %v, want PATH once", got)
	}
}

func TestArgWithoutDefaultIsAReference(t *testing.T) {
	// A bare ARG must be supplied with --build-arg, so it supplies nothing.
	res := parse(t, "ARG BUILD_VERSION\n")

	occ := find(t, res, "BUILD_VERSION")
	if occ.Kind != parser.KindReference {
		t.Errorf("kind = %q, want %q", occ.Kind, parser.KindReference)
	}
	if occ.HasDefault {
		t.Error("a bare ARG should not be marked as having a default")
	}
}

func TestArgWithDefaultIsADefinition(t *testing.T) {
	res := parse(t, "ARG NODE_VERSION=20\n")
	assertDefinition(t, res, "NODE_VERSION", "20")
}

func TestLineContinuation(t *testing.T) {
	res := parse(t, "ENV A=1 \\\n    B=2 \\\n    C=3\n")

	for name, value := range map[string]string{"A": "1", "B": "2", "C": "3"} {
		assertDefinition(t, res, name, value)
	}
	// The whole instruction is attributed to the line it started on.
	if got := find(t, res, "C").Location.Line; got != 1 {
		t.Errorf("C line = %d, want 1", got)
	}
}

func TestComments(t *testing.T) {
	res := parse(t, "# ENV COMMENTED=1\nENV REAL=2\n")

	if got := names(res); len(got) != 1 || got[0] != "REAL" {
		t.Errorf("names = %v, want only REAL", got)
	}
}

func TestCommentInsideAContinuation(t *testing.T) {
	res := parse(t, "ENV A=1 \\\n# a note\n    B=2\n")

	for name, value := range map[string]string{"A": "1", "B": "2"} {
		assertDefinition(t, res, name, value)
	}
}

func TestLineNumbers(t *testing.T) {
	res := parse(t, "FROM alpine\n\n# comment\nENV A=1\nARG B\n")

	if got := find(t, res, "A").Location.Line; got != 4 {
		t.Errorf("A line = %d, want 4", got)
	}
	if got := find(t, res, "B").Location.Line; got != 5 {
		t.Errorf("B line = %d, want 5", got)
	}
}

func TestLowercaseInstructions(t *testing.T) {
	res := parse(t, "from alpine\nenv A=1\narg B\n")

	if len(res.Occurrences) != 2 {
		t.Errorf("occurrences = %+v, want A and B: instructions are case-insensitive", res.Occurrences)
	}
}

func TestOtherInstructionsAreIgnored(t *testing.T) {
	res := parse(t, `FROM node:20
WORKDIR /app
COPY . .
RUN npm ci
EXPOSE 3000
CMD ["node", "server.js"]
LABEL maintainer="someone"
`)

	if len(res.Occurrences) != 0 {
		t.Errorf("occurrences = %+v, want none", res.Occurrences)
	}
}

func TestMalformedLines(t *testing.T) {
	for _, input := range []string{
		"ENV\n",
		"ENV \n",
		"ARG\n",
		"ENV 1BAD=x\n",
		"ARG =nokey\n",
		"",
		"\n\n\n",
	} {
		if res := parse(t, input); len(res.Occurrences) != 0 {
			t.Errorf("Parse(%q) = %+v, want nothing", input, res.Occurrences)
		}
	}
}

func TestWindowsLineEndings(t *testing.T) {
	res := parse(t, "FROM alpine\r\nENV A=1\r\n")

	assertDefinition(t, res, "A", "1")
	if got := find(t, res, "A").Location.Line; got != 2 {
		t.Errorf("A line = %d, want 2", got)
	}
}

func TestNoServicesReported(t *testing.T) {
	if res := parse(t, "ENV A=1\n"); len(res.Services) != 0 {
		t.Errorf("services = %+v, want none", res.Services)
	}
}
