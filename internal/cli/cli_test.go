package cli_test

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PeacexF/EnvGraph/internal/cli"
)

// result is what one CLI invocation produced.
type result struct {
	code   int
	stdout string
	stderr string
}

func (r result) String() string {
	return "exit " + string(rune('0'+r.code)) + "\nstdout:\n" + r.stdout + "\nstderr:\n" + r.stderr
}

func run(args ...string) result {
	var stdout, stderr bytes.Buffer
	code := cli.Run(args, &stdout, &stderr)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// project writes files into a temporary directory and returns its path.
func project(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// sample is a project with one healthy variable, one missing, one unused.
func sample(t *testing.T) string {
	t.Helper()

	return project(t, map[string]string{
		".env": "DATABASE_URL=postgres://localhost\nOLD_KEY=stale\n",
		"docker-compose.yml": "services:\n  api:\n    environment:\n" +
			"      DATABASE_URL: ${DATABASE_URL}\n",
		"main.go": "package main\n\nimport \"os\"\n\nfunc main() {\n" +
			"\t_ = os.Getenv(\"DATABASE_URL\")\n" +
			"\t_ = os.Getenv(\"JWT_SECRET\")\n}\n",
	})
}

func assertContains(t *testing.T, got string, want ...string) {
	t.Helper()

	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output does not contain %q:\n%s", w, got)
		}
	}
}

func TestScanPrintsTheFlow(t *testing.T) {
	res := run("scan", sample(t))

	if res.code != 0 {
		t.Errorf("exit code = %d, want 0: %s", res.code, res)
	}
	assertContains(t, res.stdout,
		"DATABASE_URL", "source", ".env:1",
		"passed to", "api",
		"used in", "main.go:6",
		"JWT_SECRET", "missing",
		"OLD_KEY", "unused",
		"1 ok, 1 missing, 1 unused",
	)
}

func TestScanHidesValuesByDefault(t *testing.T) {
	root := project(t, map[string]string{".env": "TOKEN=s3cret\n"})

	if res := run("scan", root); strings.Contains(res.stdout, "s3cret") {
		t.Errorf("scan printed a secret without being asked:\n%s", res.stdout)
	}

	res := run("scan", root, "--show-values")
	assertContains(t, res.stdout, "s3cret")
}

func TestScanDefaultsToTheCurrentDirectory(t *testing.T) {
	root := project(t, map[string]string{".env": "ONLY_HERE=1\n"})
	t.Chdir(root)

	res := run("scan")
	if res.code != 0 {
		t.Errorf("exit code = %d, want 0: %s", res.code, res)
	}
	assertContains(t, res.stdout, "ONLY_HERE")
}

func TestScanOnAProjectWithNoVariables(t *testing.T) {
	res := run("scan", t.TempDir())

	if res.code != 0 {
		t.Errorf("exit code = %d, want 0: %s", res.code, res)
	}
	assertContains(t, res.stdout, "No environment variables found")
}

func TestScanJSON(t *testing.T) {
	res := run("scan", sample(t), "--format", "json")

	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", res.code, res)
	}

	var doc struct {
		Files []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"files"`
		Variables []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"variables"`
		Graph struct {
			Nodes []struct {
				ID string `json:"id"`
			} `json:"nodes"`
			Edges []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"edges"`
		} `json:"graph"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, res.stdout)
	}

	if len(doc.Files) != 3 {
		t.Errorf("files = %+v, want three", doc.Files)
	}
	if len(doc.Variables) != 3 {
		t.Errorf("variables = %+v, want three", doc.Variables)
	}
	if len(doc.Graph.Nodes) == 0 || len(doc.Graph.Edges) == 0 {
		t.Errorf("graph = %+v, want nodes and edges", doc.Graph)
	}
}

func TestScanRejectsAnUnknownFormat(t *testing.T) {
	res := run("scan", sample(t), "--format", "xml")

	if res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
	assertContains(t, res.stderr, "unknown format")
}

func TestScanWritesToAFile(t *testing.T) {
	root := sample(t)
	out := filepath.Join(t.TempDir(), "report.json")

	res := run("scan", root, "--format", "json", "-o", out)
	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", res.code, res)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want everything in the file", res.stdout)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !json.Valid(data) {
		t.Errorf("file is not valid JSON:\n%s", data)
	}
}

func TestOutputFileErrorsAreReported(t *testing.T) {
	res := run("scan", sample(t), "-o", filepath.Join(t.TempDir(), "missing", "report.txt"))

	if res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
	assertContains(t, res.stderr, "error:")
}

func TestScanReportsUnreadableFilesWithoutFailing(t *testing.T) {
	root := project(t, map[string]string{
		".env":               "A=1\n",
		"docker-compose.yml": "services:\n  - [unclosed\n",
	})

	res := run("scan", root)
	if res.code != 0 {
		t.Errorf("exit code = %d, want 0: a malformed file is a warning", res.code)
	}
	assertContains(t, res.stderr, "warning:")
	assertContains(t, res.stdout, "A")
}

func TestCheckFailsOnMissing(t *testing.T) {
	res := run("check", sample(t))

	if res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
	assertContains(t, res.stdout,
		"ERROR", "JWT_SECRET is used but never provided", "main.go:7",
		"WARNING", "OLD_KEY is defined but never used", ".env:2",
		"1 missing, 1 unused",
	)
}

func TestCheckPassesWhenEverythingResolves(t *testing.T) {
	root := project(t, map[string]string{
		".env":    "PORT=8080\n",
		"main.go": "package main\nimport \"os\"\nfunc main() { _ = os.Getenv(\"PORT\") }\n",
	})

	res := run("check", root)
	if res.code != 0 {
		t.Errorf("exit code = %d, want 0: %s", res.code, res)
	}
	assertContains(t, res.stdout, "All 1 variables are provided and used")
}

func TestCheckTreatsUnusedAsAWarning(t *testing.T) {
	root := project(t, map[string]string{".env": "SPARE=1\n"})

	res := run("check", root)
	if res.code != 0 {
		t.Errorf("exit code = %d, want 0 without --strict", res.code)
	}
	assertContains(t, res.stdout, "WARNING", "1 unused")
}

func TestCheckStrictFailsOnUnused(t *testing.T) {
	root := project(t, map[string]string{".env": "SPARE=1\n"})

	if res := run("check", root, "--strict"); res.code != 1 {
		t.Errorf("exit code = %d, want 1 with --strict", res.code)
	}
}

func TestCheckStrictStillPassesWhenClean(t *testing.T) {
	root := project(t, map[string]string{
		".env":    "PORT=8080\n",
		"main.go": "package main\nimport \"os\"\nfunc main() { _ = os.Getenv(\"PORT\") }\n",
	})

	if res := run("check", root, "--strict"); res.code != 0 {
		t.Errorf("exit code = %d, want 0", res.code)
	}
}

func TestExportWritesGraphJSON(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "graph.json")

	res := run("export", sample(t), "-o", out)
	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", res.code, res)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var doc struct {
		Nodes []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"nodes"`
		Edges []struct {
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, data)
	}

	if len(doc.Nodes) == 0 || len(doc.Edges) == 0 {
		t.Fatalf("graph = %+v, want nodes and edges", doc)
	}

	// The document must carry no analysis wrapper: it is the graph itself.
	if strings.Contains(string(data), `"variables"`) {
		t.Error("export wrote the analysis too; that is what scan --format json is for")
	}
}

func TestExportToStdout(t *testing.T) {
	res := run("export", sample(t), "-o", "-")

	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", res.code, res)
	}
	if !json.Valid([]byte(res.stdout)) {
		t.Errorf("stdout is not valid JSON:\n%s", res.stdout)
	}
}

func TestExportIsReproducible(t *testing.T) {
	root := sample(t)

	first := run("export", root, "-o", "-")
	second := run("export", root, "-o", "-")

	if first.stdout != second.stdout {
		t.Error("two exports of an unchanged project differ")
	}
}

func TestExcludeFlag(t *testing.T) {
	root := project(t, map[string]string{
		".env":          "KEPT=1\n",
		"fixtures/.env": "DROPPED=2\n",
	})

	res := run("scan", root, "--exclude", "fixtures")
	assertContains(t, res.stdout, "KEPT")

	if strings.Contains(res.stdout, "DROPPED") {
		t.Errorf("--exclude did not skip the directory:\n%s", res.stdout)
	}
}

func TestIncludeTestsFlag(t *testing.T) {
	root := project(t, map[string]string{
		".env":         "FROM_TEST=1\n",
		"main_test.go": "package main\nimport \"os\"\nfunc TestX() { _ = os.Getenv(\"FROM_TEST\") }\n",
	})

	if res := run("check", root); res.code != 0 || !strings.Contains(res.stdout, "unused") {
		t.Errorf("without --include-tests the variable should look unused:\n%s", res.stdout)
	}

	res := run("check", root, "--include-tests")
	assertContains(t, res.stdout, "All 1 variables are provided and used")
}

func TestMissingPathIsAnError(t *testing.T) {
	res := run("scan", filepath.Join(t.TempDir(), "nowhere"))

	if res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
	assertContains(t, res.stderr, "error:")
}

func TestTooManyArguments(t *testing.T) {
	if res := run("scan", ".", "extra"); res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
}

func TestUnknownCommand(t *testing.T) {
	// Must be a name no command will ever take: a real one would run, and
	// `serve` in particular would block the suite forever.
	res := run("visualise")

	if res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
	assertContains(t, res.stderr, "error:")
}

func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"scan", "--help"}, {"--version"}} {
		res := run(args...)
		if res.code != 0 {
			t.Errorf("%v: exit code = %d, want 0", args, res.code)
		}
		if res.stdout == "" {
			t.Errorf("%v: printed nothing", args)
		}
	}
}

func TestNoArgumentsPrintsHelp(t *testing.T) {
	res := run()

	if res.code != 0 {
		t.Errorf("exit code = %d, want 0", res.code)
	}
	assertContains(t, res.stdout, "envgraph", "scan", "check", "export")
}

func TestOutputIsPlainWhenNotATerminal(t *testing.T) {
	res := run("scan", sample(t))

	if strings.Contains(res.stdout, "\033[") {
		t.Errorf("output contains escape codes despite not being a terminal:\n%q", res.stdout)
	}
}

func TestScanLabelsDerivedAndDefaultSources(t *testing.T) {
	root := project(t, map[string]string{
		".env": "POSTGRES_HOST=db\n",
		"docker-compose.yml": "services:\n  web:\n    environment:\n" +
			"      DB_HOST: ${POSTGRES_HOST}\n" +
			"      PORT: ${PORT:-8080}\n",
	})

	res := run("scan", root)
	assertContains(t, res.stdout,
		"derived", "from POSTGRES_HOST",
		"default",
	)
}

func TestCheckShowsWhereAMissingVariableIsPassed(t *testing.T) {
	root := project(t, map[string]string{
		"docker-compose.yml": "services:\n  api:\n    environment:\n" +
			"      SECRET: ${SECRET}\n",
	})

	res := run("check", root)
	if res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
	assertContains(t, res.stdout, "SECRET is used but never provided", "passed to api")
}

func TestServeRejectsAMissingPath(t *testing.T) {
	res := run("serve", filepath.Join(t.TempDir(), "nowhere"))

	if res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
	assertContains(t, res.stderr, "error:")
}

func TestServeReportsAPortItCannotBind(t *testing.T) {
	// Occupy a port, then ask serve for the same one.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	res := run("serve", sample(t), "--port", port)
	if res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
	assertContains(t, res.stderr, "listen on")
}

func TestServeHelpDocumentsItsDefaults(t *testing.T) {
	res := run("serve", "--help")

	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0", res.code)
	}
	assertContains(t, res.stdout, "--port", "--host", "--show-values", "localhost")
}

func TestIgnoreFlag(t *testing.T) {
	root := project(t, map[string]string{".env": "KEPT=1\nDROPPED=2\n"})

	res := run("scan", root, "--ignore", "DROPPED")
	assertContains(t, res.stdout, "KEPT")

	if strings.Contains(res.stdout, "DROPPED") {
		t.Errorf("--ignore did not drop the variable:\n%s", res.stdout)
	}
}

func TestIgnoreFlagAcceptsGlobs(t *testing.T) {
	root := project(t, map[string]string{".env": "VITE_A=1\nVITE_B=2\nKEPT=3\n"})

	res := run("scan", root, "--ignore", "VITE_*")
	if strings.Contains(res.stdout, "VITE_") {
		t.Errorf("the glob did not drop the variables:\n%s", res.stdout)
	}
	assertContains(t, res.stdout, "KEPT")
}

func TestSystemVariablesAreIgnoredByDefault(t *testing.T) {
	root := project(t, map[string]string{"Dockerfile": "ENV PATH=/opt/bin\nENV REAL=1\n"})

	res := run("scan", root)
	if strings.Contains(res.stdout, "PATH") {
		t.Errorf("PATH should not be reported by default:\n%s", res.stdout)
	}
	assertContains(t, res.stdout, "REAL")
}

func TestNoConfigBringsSystemVariablesBack(t *testing.T) {
	root := project(t, map[string]string{"Dockerfile": "ENV PATH=/opt/bin\n"})

	assertContains(t, run("scan", root, "--no-config").stdout, "PATH")
}

func TestConfigFileIsPickedUpFromTheRoot(t *testing.T) {
	root := project(t, map[string]string{
		".envgraph.yml": "exclude:\n  - fixtures\nignore:\n  - OLD_KEY\n",
		".env":          "KEPT=1\nOLD_KEY=2\n",
		"fixtures/.env": "FROM_FIXTURE=3\n",
	})

	res := run("scan", root)
	assertContains(t, res.stdout, "KEPT")

	for _, gone := range []string{"OLD_KEY", "FROM_FIXTURE"} {
		if strings.Contains(res.stdout, gone) {
			t.Errorf("%s survived the config:\n%s", gone, res.stdout)
		}
	}
}

func TestNoConfigSkipsTheFile(t *testing.T) {
	root := project(t, map[string]string{
		".envgraph.yml": "ignore:\n  - OLD_KEY\n",
		".env":          "OLD_KEY=1\n",
	})

	assertContains(t, run("scan", root, "--no-config").stdout, "OLD_KEY")
}

func TestExplicitConfigPath(t *testing.T) {
	root := project(t, map[string]string{".env": "OLD_KEY=1\nKEPT=2\n"})
	other := project(t, map[string]string{"rules.yml": "ignore:\n  - OLD_KEY\n"})

	res := run("scan", root, "--config", filepath.Join(other, "rules.yml"))
	assertContains(t, res.stdout, "KEPT")

	if strings.Contains(res.stdout, "OLD_KEY") {
		t.Errorf("the named config was not applied:\n%s", res.stdout)
	}
}

func TestBadConfigIsReported(t *testing.T) {
	root := project(t, map[string]string{".envgraph.yml": "ignore: [unclosed\n"})

	res := run("scan", root)
	if res.code != 1 {
		t.Errorf("exit code = %d, want 1", res.code)
	}
	assertContains(t, res.stderr, "error:")
}

func TestIgnoredVariablesLeaveTheGraph(t *testing.T) {
	root := project(t, map[string]string{".env": "KEPT=1\nDROPPED=2\n"})

	res := run("export", root, "-o", "-", "--ignore", "DROPPED")
	if res.code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", res.code, res)
	}
	if strings.Contains(res.stdout, "DROPPED") {
		t.Errorf("an ignored variable is still in the graph:\n%s", res.stdout)
	}
	assertContains(t, res.stdout, "KEPT")
}

func TestIgnoredVariablesCannotFailCheck(t *testing.T) {
	root := project(t, map[string]string{
		"main.go": "package main\nimport \"os\"\nfunc main() { _ = os.Getenv(\"ABSENT\") }\n",
	})

	if res := run("check", root); res.code != 1 {
		t.Fatalf("exit code = %d, want 1 before ignoring", res.code)
	}
	if res := run("check", root, "--ignore", "ABSENT"); res.code != 0 {
		t.Errorf("exit code = %d, want 0 once ignored", res.code)
	}
}
