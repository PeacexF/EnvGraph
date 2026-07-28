package workflow_test

import (
	"testing"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/workflow"
)

func parse(t *testing.T, content string) parser.Result {
	t.Helper()

	res, err := workflow.Parse(".github/workflows/ci.yml", []byte(content))
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

func names(res parser.Result) map[string]bool {
	out := make(map[string]bool)
	for _, occ := range res.Occurrences {
		out[occ.Name] = true
	}
	return out
}

// job wraps a single-job workflow around a body.
func job(body string) string {
	return "name: CI\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n" + body
}

func TestWorkflowLevelEnv(t *testing.T) {
	res := parse(t, "name: CI\non: push\nenv:\n  GO_VERSION: \"1.26\"\njobs:\n  build:\n    runs-on: ubuntu-latest\n")

	occ := find(t, res, "GO_VERSION")
	if occ.Kind != parser.KindDefinition || occ.Value != "1.26" {
		t.Errorf("GO_VERSION = %+v, want a definition with value 1.26", occ)
	}
	// Workflow-level env belongs to the file, not to any one job.
	if occ.Service != "" {
		t.Errorf("service = %q, want empty for workflow-level env", occ.Service)
	}
}

func TestJobLevelEnv(t *testing.T) {
	res := parse(t, job("    env:\n      CGO_ENABLED: \"0\"\n"))

	occ := find(t, res, "CGO_ENABLED")
	if occ.Kind != parser.KindDefinition || occ.Service != "build" {
		t.Errorf("CGO_ENABLED = %+v, want a definition on the build job", occ)
	}
}

func TestStepLevelEnv(t *testing.T) {
	res := parse(t, job("    steps:\n      - run: make\n        env:\n          TARGET: release\n"))

	occ := find(t, res, "TARGET")
	if occ.Kind != parser.KindDefinition || occ.Service != "build" {
		t.Errorf("TARGET = %+v, want a definition on the build job", occ)
	}
}

func TestJobsAreServices(t *testing.T) {
	res := parse(t, "on: push\njobs:\n  lint:\n    runs-on: ubuntu-latest\n  test:\n    runs-on: ubuntu-latest\n")

	if len(res.Services) != 2 {
		t.Fatalf("services = %+v, want lint and test", res.Services)
	}
	if res.Services[0].Name != "lint" || res.Services[1].Name != "test" {
		t.Errorf("services = %+v, want lint then test", res.Services)
	}
	if res.Services[0].Location.File != ".github/workflows/ci.yml" {
		t.Errorf("file = %q, want the workflow path", res.Services[0].Location.File)
	}
}

func TestSecretsSupplyTheValue(t *testing.T) {
	res := parse(t, job("    env:\n      API_KEY: ${{ secrets.API_KEY }}\n"))

	occ := find(t, res, "API_KEY")
	if occ.Kind != parser.KindDefinition {
		t.Errorf("kind = %q, want %q: GitHub supplies the secret", occ.Kind, parser.KindDefinition)
	}
	if occ.Origin != workflow.OriginSecret {
		t.Errorf("origin = %q, want %q", occ.Origin, workflow.OriginSecret)
	}
	if occ.Value != "" {
		t.Errorf("value = %q, want empty: a secret's value is not in the repository", occ.Value)
	}
}

func TestRepositoryVariablesAndInputs(t *testing.T) {
	res := parse(t, job("    env:\n      REGION: ${{ vars.AWS_REGION }}\n      MODE: ${{ inputs.mode }}\n"))

	if occ := find(t, res, "REGION"); occ.Origin != workflow.OriginVar {
		t.Errorf("REGION origin = %q, want %q", occ.Origin, workflow.OriginVar)
	}
	if occ := find(t, res, "MODE"); occ.Origin != workflow.OriginInput {
		t.Errorf("MODE origin = %q, want %q", occ.Origin, workflow.OriginInput)
	}
}

func TestRenamedFromAnotherEnvVariable(t *testing.T) {
	res := parse(t, "on: push\nenv:\n  BASE: value\njobs:\n  build:\n    runs-on: ubuntu-latest\n    env:\n      DERIVED: ${{ env.BASE }}\n")

	occ := find(t, res, "DERIVED")
	if occ.Kind != parser.KindReference {
		t.Errorf("kind = %q, want %q", occ.Kind, parser.KindReference)
	}
	if len(occ.DerivedFrom) != 1 || occ.DerivedFrom[0] != "BASE" {
		t.Errorf("derivedFrom = %v, want [BASE]", occ.DerivedFrom)
	}
}

func TestExpressionsAreReadAnywhere(t *testing.T) {
	// ${{ env.X }} is allowed in with:, if: and name:, not just env blocks.
	res := parse(t, job(`    steps:
      - uses: actions/setup-go@v6
        with:
          go-version: ${{ env.GO_VERSION }}
      - if: ${{ env.DEPLOY == 'true' }}
        name: Deploy ${{ env.TARGET }}
        run: ./deploy
`))

	for _, name := range []string{"GO_VERSION", "DEPLOY", "TARGET"} {
		occ := find(t, res, name)
		if occ.Kind != parser.KindConsumption {
			t.Errorf("%s kind = %q, want %q", name, occ.Kind, parser.KindConsumption)
		}
		if occ.Service != "build" {
			t.Errorf("%s service = %q, want build", name, occ.Service)
		}
	}
}

func TestRunScriptsReadShellVariables(t *testing.T) {
	res := parse(t, job("    steps:\n      - run: echo \"$DATABASE_URL and ${REDIS_URL}\"\n"))

	for _, name := range []string{"DATABASE_URL", "REDIS_URL"} {
		occ := find(t, res, name)
		if occ.Kind != parser.KindConsumption || occ.Service != "build" {
			t.Errorf("%s = %+v, want a consumption on the build job", name, occ)
		}
	}
}

func TestShellLocalsAreNotVariables(t *testing.T) {
	res := parse(t, job(`    steps:
      - run: |
          unformatted=$(gofmt -l .)
          echo "$unformatted"

          server &
          pid=$!
          kill $pid

          for example in examples/*/; do
            echo "$example"
          done

          export LOCAL_EXPORT=1
          echo "$LOCAL_EXPORT"

          read answer
          echo "$answer"

          echo "$REAL_VARIABLE"
`))

	got := names(res)
	for _, local := range []string{"unformatted", "pid", "example", "LOCAL_EXPORT", "answer"} {
		if got[local] {
			t.Errorf("%s is a shell local, not configuration", local)
		}
	}
	if !got["REAL_VARIABLE"] {
		t.Errorf("names = %v, want REAL_VARIABLE kept", got)
	}
}

func TestGitHubExpressionsAreNotShellSyntax(t *testing.T) {
	// ${{ secrets.X }} is substituted before the shell runs, so it must not
	// be read as a shell reference to a variable called "{".
	res := parse(t, job("    steps:\n      - run: deploy --key ${{ secrets.KEY }}\n"))

	for _, occ := range res.Occurrences {
		if occ.Name == "KEY" || occ.Name == "" {
			t.Errorf("occurrence %+v, want the expression left alone", occ)
		}
	}
}

func TestMultilineScript(t *testing.T) {
	res := parse(t, job("    steps:\n      - run: |\n          echo $FIRST\n          echo $SECOND\n"))

	for _, name := range []string{"FIRST", "SECOND"} {
		find(t, res, name)
	}
}

func TestSameVariableTwiceInAScript(t *testing.T) {
	res := parse(t, job("    steps:\n      - run: echo $SAME $SAME\n"))

	count := 0
	for _, occ := range res.Occurrences {
		if occ.Name == "SAME" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("got %d occurrences of SAME, want 1", count)
	}
}

func TestLineNumbers(t *testing.T) {
	res := parse(t, "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    env:\n      A: 1\n")

	if got := res.Services[0].Location.Line; got != 3 {
		t.Errorf("job line = %d, want 3", got)
	}
	if got := find(t, res, "A").Location.Line; got != 6 {
		t.Errorf("A line = %d, want 6", got)
	}
}

func TestFilesWithoutJobs(t *testing.T) {
	for _, input := range []string{
		"",
		"name: CI\n",
		"on: push\n",
		"on: push\njobs:\n",
		"on: push\njobs:\n  build: null\n",
	} {
		res := parse(t, input)
		if len(res.Services) != 0 {
			t.Errorf("Parse(%q) services = %+v, want none", input, res.Services)
		}
	}
}

func TestNonScalarEnvValuesAreSkipped(t *testing.T) {
	res := parse(t, job("    env:\n      NESTED:\n        deep: value\n      FINE: ok\n"))

	got := names(res)
	if got["NESTED"] || !got["FINE"] {
		t.Errorf("names = %v, want only FINE", got)
	}
}

func TestInvalidYAML(t *testing.T) {
	if _, err := workflow.Parse(".github/workflows/ci.yml", []byte("jobs:\n  - [unclosed\n")); err == nil {
		t.Error("Parse() error = nil, want an error for malformed yaml")
	}
}
