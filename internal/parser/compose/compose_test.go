package compose_test

import (
	"testing"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/compose"
)

func parse(t *testing.T, yaml string) parser.Result {
	t.Helper()

	res, err := compose.Parse("docker-compose.yml", []byte(yaml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return res
}

// find returns the first occurrence for a name, or fails the test.
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

// names returns every variable name mentioned, in order.
func names(res parser.Result) []string {
	out := make([]string, 0, len(res.Occurrences))
	for _, occ := range res.Occurrences {
		out = append(out, occ.Name)
	}
	return out
}

// service wraps a single-service compose file around an environment block.
func service(env string) string {
	return "services:\n  api:\n    environment:\n" + env
}

func TestMappingEnvironment(t *testing.T) {
	res := parse(t, service(
		"      LOG_LEVEL: info\n"+
			"      DATABASE_URL: ${DATABASE_URL}\n"+
			"      PORT: ${PORT:-8080}\n"))

	if occ := find(t, res, "LOG_LEVEL"); occ.Kind != parser.KindDefinition || occ.Value != "info" {
		t.Errorf("LOG_LEVEL = %+v, want a definition with value info", occ)
	}

	occ := find(t, res, "DATABASE_URL")
	if occ.Kind != parser.KindReference {
		t.Errorf("DATABASE_URL kind = %q, want %q", occ.Kind, parser.KindReference)
	}
	if occ.HasDefault {
		t.Error("DATABASE_URL should not be marked as having a default")
	}
	if occ.Service != "api" {
		t.Errorf("DATABASE_URL service = %q, want api", occ.Service)
	}

	if occ := find(t, res, "PORT"); !occ.HasDefault {
		t.Errorf("PORT = %+v, want HasDefault", occ)
	}
}

func TestListEnvironment(t *testing.T) {
	res := parse(t, "services:\n  worker:\n    environment:\n"+
		"      - QUEUE=jobs\n"+
		"      - INHERITED\n"+
		"      - SPACED = value\n")

	if occ := find(t, res, "QUEUE"); occ.Kind != parser.KindDefinition || occ.Value != "jobs" {
		t.Errorf("QUEUE = %+v, want a definition with value jobs", occ)
	}

	// "- NAME" forwards a host variable and supplies nothing.
	occ := find(t, res, "INHERITED")
	if occ.Kind != parser.KindReference || occ.HasDefault {
		t.Errorf("INHERITED = %+v, want a reference with no default", occ)
	}
	if occ.Service != "worker" {
		t.Errorf("INHERITED service = %q, want worker", occ.Service)
	}

	if occ := find(t, res, "SPACED"); occ.Kind != parser.KindDefinition {
		t.Errorf("SPACED = %+v, want a definition", occ)
	}
}

func TestNumericAndBooleanValues(t *testing.T) {
	res := parse(t, service("      REPLICAS: 3\n      DEBUG: true\n      RATIO: 1.5\n"))

	for name, want := range map[string]string{"REPLICAS": "3", "DEBUG": "true", "RATIO": "1.5"} {
		occ := find(t, res, name)
		if occ.Kind != parser.KindDefinition || occ.Value != want {
			t.Errorf("%s = %+v, want a definition with value %q", name, occ, want)
		}
	}
}

func TestInterpolationForms(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		refs       []string
		hasDefault bool
	}{
		{"braced", "${VAR}", []string{"VAR"}, false},
		{"bare", "$VAR", []string{"VAR"}, false},
		{"colon dash default", "${VAR:-x}", []string{"VAR"}, true},
		{"dash default", "${VAR-x}", []string{"VAR"}, true},
		{"required", "${VAR:?must be set}", []string{"VAR"}, false},
		{"alternate", "${VAR:+other}", []string{"VAR"}, false},
		// Nothing is guaranteed here: if neither is set the value is empty.
		{"nested default", "${VAR:-${FALLBACK}}", []string{"VAR", "FALLBACK"}, false},
		{"two references", "${HOST}:${PORT}", []string{"HOST", "PORT"}, false},
		{"inside a url", "postgres://${USER}@${HOST}/db", []string{"USER", "HOST"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := parse(t, service("      TARGET: \""+tt.value+"\"\n"))

			for _, name := range tt.refs {
				occ := find(t, res, name)
				if occ.Kind != parser.KindReference {
					t.Errorf("%s kind = %q, want %q", name, occ.Kind, parser.KindReference)
				}
			}

			// TARGET is the derived key: it resolves when its inputs do.
			target := find(t, res, "TARGET")
			if target.HasDefault != tt.hasDefault {
				t.Errorf("TARGET hasDefault = %v, want %v", target.HasDefault, tt.hasDefault)
			}
		})
	}
}

func TestEscapedDollarIsNotAReference(t *testing.T) {
	res := parse(t, service("      PRICE: \"$$100\"\n"))

	if got := names(res); len(got) != 1 || got[0] != "PRICE" {
		t.Errorf("names = %v, want only PRICE: $$ is a literal dollar sign", got)
	}
	if occ := find(t, res, "PRICE"); occ.Kind != parser.KindDefinition {
		t.Errorf("PRICE = %+v, want a definition", occ)
	}
}

func TestSelfReferenceIsNotDerived(t *testing.T) {
	res := parse(t, service("      DATABASE_URL: ${DATABASE_URL}\n"))

	occ := find(t, res, "DATABASE_URL")
	if len(occ.DerivedFrom) != 0 {
		t.Errorf("DATABASE_URL derivedFrom = %v, want none: it refers to itself", occ.DerivedFrom)
	}
	if len(res.Occurrences) != 1 {
		t.Errorf("occurrences = %+v, want one: the key and the reference are the same variable",
			res.Occurrences)
	}
}

func TestRenamedVariableIsDerived(t *testing.T) {
	res := parse(t, service("      DB_HOST: ${POSTGRES_HOST}\n"))

	if occ := find(t, res, "POSTGRES_HOST"); occ.Kind != parser.KindReference {
		t.Errorf("POSTGRES_HOST kind = %q, want %q", occ.Kind, parser.KindReference)
	}

	occ := find(t, res, "DB_HOST")
	if len(occ.DerivedFrom) != 1 || occ.DerivedFrom[0] != "POSTGRES_HOST" {
		t.Errorf("DB_HOST derivedFrom = %v, want [POSTGRES_HOST]", occ.DerivedFrom)
	}
	if occ.Service != "api" {
		t.Errorf("DB_HOST service = %q, want api", occ.Service)
	}
}

func TestRenamedVariableWithDefaultIsSelfSufficient(t *testing.T) {
	res := parse(t, service("      DB_HOST: ${POSTGRES_HOST:-localhost}\n"))

	occ := find(t, res, "DB_HOST")
	if !occ.HasDefault {
		t.Errorf("DB_HOST = %+v, want HasDefault: the fallback guarantees a value", occ)
	}
	if len(occ.DerivedFrom) != 0 {
		t.Errorf("DB_HOST derivedFrom = %v, want none", occ.DerivedFrom)
	}
}

func TestRenamedVariableFromSeveralInputs(t *testing.T) {
	res := parse(t, service("      DSN: \"${USER}:${PASS}@host\"\n"))

	occ := find(t, res, "DSN")
	if len(occ.DerivedFrom) != 2 {
		t.Errorf("DSN derivedFrom = %v, want both USER and PASS", occ.DerivedFrom)
	}
}

func TestServices(t *testing.T) {
	res := parse(t, "services:\n  api:\n    image: x\n  db:\n    image: postgres\n")

	if len(res.Services) != 2 {
		t.Fatalf("got %d services, want 2: %+v", len(res.Services), res.Services)
	}
	if res.Services[0].Name != "api" || res.Services[1].Name != "db" {
		t.Errorf("services = %+v, want api then db", res.Services)
	}
	if res.Services[0].Location.File != "docker-compose.yml" {
		t.Errorf("service file = %q, want docker-compose.yml", res.Services[0].Location.File)
	}
}

func TestEnvFilesResolveAgainstTheComposeDirectory(t *testing.T) {
	res, err := compose.Parse("deploy/docker-compose.yml", []byte(
		"services:\n"+
			"  api:\n"+
			"    env_file:\n"+
			"      - .env\n"+
			"      - ../shared.env\n"+
			"      - path: long/form.env\n"+
			"  single:\n"+
			"    env_file: only.env\n"+
			"  none:\n"+
			"    image: x\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(res.Services) != 3 {
		t.Fatalf("got %d services, want 3", len(res.Services))
	}

	want := []string{"deploy/.env", "shared.env", "deploy/long/form.env"}
	got := res.Services[0].EnvFiles
	if len(got) != len(want) {
		t.Fatalf("api env files = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("env file %d = %q, want %q", i, got[i], want[i])
		}
	}

	if files := res.Services[1].EnvFiles; len(files) != 1 || files[0] != "deploy/only.env" {
		t.Errorf("single env files = %v, want [deploy/only.env]", files)
	}
	if files := res.Services[2].EnvFiles; len(files) != 0 {
		t.Errorf("none env files = %v, want none", files)
	}
}

func TestLineNumbers(t *testing.T) {
	res := parse(t, "services:\n  api:\n    environment:\n      A: 1\n      B: 2\n")

	if got := find(t, res, "A").Location.Line; got != 4 {
		t.Errorf("A line = %d, want 4", got)
	}
	if got := find(t, res, "B").Location.Line; got != 5 {
		t.Errorf("B line = %d, want 5", got)
	}
	if got := res.Services[0].Location.Line; got != 2 {
		t.Errorf("service line = %d, want 2", got)
	}
}

func TestSeveralServicesAreKeptApart(t *testing.T) {
	res := parse(t, "services:\n"+
		"  api:\n    environment:\n      SHARED: ${SHARED}\n"+
		"  worker:\n    environment:\n      SHARED: ${SHARED}\n")

	services := make(map[string]bool)
	for _, occ := range res.Occurrences {
		if occ.Name == "SHARED" {
			services[occ.Service] = true
		}
	}

	if !services["api"] || !services["worker"] {
		t.Errorf("SHARED reached %v, want both api and worker", services)
	}
}

func TestFilesWithoutServices(t *testing.T) {
	for _, input := range []string{
		"",
		"version: '3'\n",
		"volumes:\n  data:\n",
		"services:\n",
		"services: []\n",
		"services:\n  api: null\n",
	} {
		res := parse(t, input)
		if len(res.Occurrences) != 0 || len(res.Services) != 0 {
			t.Errorf("Parse(%q) = %+v, want empty", input, res)
		}
	}
}

func TestNonScalarEnvironmentValuesAreSkipped(t *testing.T) {
	res := parse(t, service("      NESTED:\n        deep: value\n      FINE: ok\n"))

	if got := names(res); len(got) != 1 || got[0] != "FINE" {
		t.Errorf("names = %v, want only FINE", got)
	}
}

func TestInvalidYAML(t *testing.T) {
	if _, err := compose.Parse("docker-compose.yml", []byte("services:\n  - [unclosed\n")); err == nil {
		t.Error("Parse() error = nil, want an error for malformed yaml")
	}
}

func TestMalformedInterpolation(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{"unclosed brace", "${VAR", nil},
		{"unclosed brace after text", "prefix ${VAR", nil},
		{"lone dollar at the end", "value$", nil},
		{"empty braces", "${}", nil},
		{"dollar before punctuation", "$-nope", nil},
		{"name starting with a digit", "${1BAD}", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := parse(t, service("      TARGET: \""+tt.value+"\"\n"))

			for _, occ := range res.Occurrences {
				if occ.Name != "TARGET" {
					t.Errorf("found %q, want nothing beyond the key itself", occ.Name)
				}
			}
		})
	}
}

func TestEmptyKeysAreSkipped(t *testing.T) {
	res := parse(t, "services:\n  api:\n    environment:\n      - =orphan\n      - REAL=1\n")

	if got := names(res); len(got) != 1 || got[0] != "REAL" {
		t.Errorf("names = %v, want only REAL", got)
	}
}

func TestNonMappingServiceBody(t *testing.T) {
	res := parse(t, "services:\n  api: image-name\n  db:\n    environment:\n      A: 1\n")

	if len(res.Services) != 1 || res.Services[0].Name != "db" {
		t.Errorf("services = %+v, want only db: api has no mapping body", res.Services)
	}
}

func TestEnvironmentThatIsNotAMapOrList(t *testing.T) {
	res := parse(t, "services:\n  api:\n    environment: \"A=1\"\n")

	if len(res.Occurrences) != 0 {
		t.Errorf("occurrences = %+v, want none for an unsupported shape", res.Occurrences)
	}
}
