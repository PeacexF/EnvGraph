package scanner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PeacexF/EnvGraph/internal/scanner"
)

const goSource = "package main\nimport \"os\"\nfunc main() { _ = os.Getenv(\"A\") }\n"

// project writes files into a temporary directory and returns its path
// Keys are slash-separated paths relative to the project root
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

func scan(t *testing.T, root string, opts scanner.Options) *scanner.Result {
	t.Helper()

	res, err := scanner.Scan(root, opts)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	return res
}

func paths(res *scanner.Result) []string {
	out := make([]string, 0, len(res.Files))
	for _, f := range res.Files {
		out = append(out, f.Path)
	}
	return out
}

func assertPaths(t *testing.T, res *scanner.Result, want ...string) {
	t.Helper()

	got := paths(res)
	if len(got) != len(want) {
		t.Fatalf("scanned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("file %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRecognisedFileNames(t *testing.T) {
	root := project(t, map[string]string{
		".env":                         "A=1\n",
		".env.production":              "A=2\n",
		".env.example":                 "A=3\n",
		"prod.env":                     "A=4\n",
		"docker-compose.yml":           "services:\n  api:\n    image: x\n",
		"docker-compose.override.yaml": "services:\n  api:\n    image: y\n",
		"compose.yaml":                 "services:\n  web:\n    image: z\n",
		"main.go":                      goSource,
		"app.py":                       "import os\nA = os.getenv(\"A\")\n",
	})

	assertPaths(t, scan(t, root, scanner.Options{}),
		".env", ".env.example", ".env.production", "app.py", "compose.yaml",
		"docker-compose.override.yaml", "docker-compose.yml", "main.go", "prod.env")
}

func TestRecognisedDockerfilesAndJavaScript(t *testing.T) {
	root := project(t, map[string]string{
		"Dockerfile":      "ENV A=1\n",
		"Dockerfile.prod": "ENV B=2\n",
		"api.Dockerfile":  "ENV C=3\n",
		"app.js":          "process.env.D\n",
		"app.jsx":         "process.env.E\n",
		"app.mjs":         "process.env.F\n",
		"app.cjs":         "process.env.G\n",
		"app.ts":          "process.env.H\n",
		"app.tsx":         "process.env.I\n",
	})

	assertPaths(t, scan(t, root, scanner.Options{}),
		"Dockerfile", "Dockerfile.prod", "api.Dockerfile",
		"app.cjs", "app.js", "app.jsx", "app.mjs", "app.ts", "app.tsx")
}

func TestDeclarationFilesAreSkipped(t *testing.T) {
	// A .d.ts holds types, never a call site.
	root := project(t, map[string]string{
		"types.d.ts": "declare const x: string\n",
		"app.ts":     "process.env.REAL\n",
	})

	assertPaths(t, scan(t, root, scanner.Options{}), "app.ts")
}

func TestJavaScriptTestFiles(t *testing.T) {
	root := project(t, map[string]string{
		"app.js":      "process.env.A\n",
		"app.test.js": "process.env.B\n",
		"app.spec.ts": "process.env.C\n",
	})

	assertPaths(t, scan(t, root, scanner.Options{}), "app.js")

	if got := len(scan(t, root, scanner.Options{IncludeTests: true}).Files); got != 3 {
		t.Errorf("scanned %d files with IncludeTests, want 3", got)
	}
}

func TestUnrecognisedFileNames(t *testing.T) {
	root := project(t, map[string]string{
		".env":                 "A=1\n",
		"README.md":            "text\n",
		"data.json":            "{}\n",
		"config.yml":           "services:\n  api:\n    image: x\n",
		"my-compose-notes.yml": "services:\n  api:\n    image: x\n",
		"environment.txt":      "A=1\n",
		"main.rs":              "fn main() {}\n",
		"Makefile":             "all:\n",
	})

	assertPaths(t, scan(t, root, scanner.Options{}), ".env")
}

func TestFileTypes(t *testing.T) {
	root := project(t, map[string]string{
		".env":               "A=1\n",
		"docker-compose.yml": "services:\n  api:\n    image: x\n",
		"main.go":            goSource,
		"app.py":             "import os\n",
		"Dockerfile":         "ENV A=1\n",
		"app.ts":             "process.env.B\n",
	})

	want := map[string]scanner.FileType{
		".env":               scanner.TypeEnv,
		"docker-compose.yml": scanner.TypeCompose,
		"main.go":            scanner.TypeGo,
		"app.py":             scanner.TypePython,
		"Dockerfile":         scanner.TypeDockerfile,
		"app.ts":             scanner.TypeJavaScript,
	}

	for _, f := range scan(t, root, scanner.Options{}).Files {
		if want[f.Path] != f.Type {
			t.Errorf("%s type = %q, want %q", f.Path, f.Type, want[f.Path])
		}
	}
}

func TestSkipsDependencyDirectories(t *testing.T) {
	root := project(t, map[string]string{
		".env":                  "A=1\n",
		"node_modules/pkg/.env": "B=2\n",
		"vendor/lib/main.go":    "package lib\n",
		".git/config":           "\n",
		"venv/lib/app.py":       "import os\n",
		"dist/.env":             "C=3\n",
		"build/.env":            "D=4\n",
		"__pycache__/app.py":    "import os\n",
		"src/config.py":         "import os\nX = os.getenv(\"X\")\n",
	})

	assertPaths(t, scan(t, root, scanner.Options{}), ".env", "src/config.py")
}

func TestExcludeOption(t *testing.T) {
	root := project(t, map[string]string{
		".env":                 "A=1\n",
		"fixtures/.env":        "B=2\n",
		"testdata/nested/.env": "C=3\n",
		"keep/.env":            "D=4\n",
	})

	res := scan(t, root, scanner.Options{Exclude: []string{"fixtures", "testdata"}})
	assertPaths(t, res, ".env", "keep/.env")
}

func TestTestFiles(t *testing.T) {
	root := project(t, map[string]string{
		"main.go":      goSource,
		"main_test.go": "package main\nimport \"os\"\nfunc TestX() { _ = os.Getenv(\"B\") }\n",
		"test_app.py":  "import os\nX = os.getenv(\"C\")\n",
		"app_test.py":  "import os\nX = os.getenv(\"D\")\n",
	})

	assertPaths(t, scan(t, root, scanner.Options{}), "main.go")

	if got := len(scan(t, root, scanner.Options{IncludeTests: true}).Files); got != 4 {
		t.Errorf("scanned %d files with IncludeTests, want 4", got)
	}
}

func TestOccurrencesAreCollected(t *testing.T) {
	root := project(t, map[string]string{
		".env":               "DATABASE_URL=postgres://localhost\n",
		"docker-compose.yml": "services:\n  api:\n    environment:\n      DATABASE_URL: ${DATABASE_URL}\n",
		"main.go":            "package main\nimport \"os\"\nfunc main() { _ = os.Getenv(\"DATABASE_URL\") }\n",
	})

	res := scan(t, root, scanner.Options{})

	if len(res.Occurrences) < 3 {
		t.Errorf("occurrences = %+v, want at least one per file", res.Occurrences)
	}
	if len(res.Services) != 1 || res.Services[0].Name != "api" {
		t.Errorf("services = %+v, want api", res.Services)
	}
}

func TestParseErrorsDoNotStopTheScan(t *testing.T) {
	root := project(t, map[string]string{
		".env":               "A=1\n",
		"docker-compose.yml": "services:\n  - [unclosed\n",
		"broken.go":          "package main\nfunc main() {",
	})

	res := scan(t, root, scanner.Options{})

	if len(res.Errors) != 2 {
		t.Errorf("errors = %v, want one per malformed file", res.Errors)
	}
	assertPaths(t, res, ".env")
}

func TestNestedPathsUseForwardSlashes(t *testing.T) {
	root := project(t, map[string]string{"cmd/api/main.go": goSource})

	res := scan(t, root, scanner.Options{})
	assertPaths(t, res, "cmd/api/main.go")

	if got := res.Occurrences[0].Location.File; got != "cmd/api/main.go" {
		t.Errorf("occurrence file = %q, want cmd/api/main.go", got)
	}
}

func TestOversizedFilesAreSkipped(t *testing.T) {
	big := strings.Repeat("PADDING_VARIABLE_NAME=some value here\n", 100_000)
	root := project(t, map[string]string{".env": big, "small.env": "A=1\n"})

	assertPaths(t, scan(t, root, scanner.Options{}), "small.env")
}

func TestEmptyProject(t *testing.T) {
	res := scan(t, t.TempDir(), scanner.Options{})

	if len(res.Files) != 0 || len(res.Occurrences) != 0 {
		t.Errorf("scan of an empty directory = %+v, want nothing", res)
	}
}

func TestRootIsRecorded(t *testing.T) {
	root := project(t, map[string]string{".env": "A=1\n"})

	if got := scan(t, root, scanner.Options{}).Root; !filepath.IsAbs(got) {
		t.Errorf("root = %q, want an absolute path", got)
	}
}

func TestFilesAreSorted(t *testing.T) {
	root := project(t, map[string]string{
		"z/.env": "A=1\n",
		"a/.env": "B=2\n",
		"m/.env": "C=3\n",
	})

	assertPaths(t, scan(t, root, scanner.Options{}), "a/.env", "m/.env", "z/.env")
}

func TestInvalidRoots(t *testing.T) {
	root := project(t, map[string]string{".env": "A=1\n"})

	if _, err := scanner.Scan(filepath.Join(root, ".env"), scanner.Options{}); err == nil {
		t.Error("Scan() error = nil, want an error for a file argument")
	}
	if _, err := scanner.Scan(filepath.Join(root, "missing"), scanner.Options{}); err == nil {
		t.Error("Scan() error = nil, want an error for a path that does not exist")
	}
}
