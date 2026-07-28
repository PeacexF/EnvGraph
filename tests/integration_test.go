package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
	"github.com/PeacexF/EnvGraph/internal/cli"
	"github.com/PeacexF/EnvGraph/internal/scanner"
	"github.com/PeacexF/EnvGraph/internal/server"
)

func examplePath(name string) string {
	return filepath.Join("..", "examples", name)
}

func analyze(t *testing.T, example string) (*scanner.Result, *analyzer.Report) {
	t.Helper()

	root := examplePath(example)
	res, err := scanner.Scan(root, scanner.Options{})
	if err != nil {
		t.Fatalf("Scan(%s) error = %v", root, err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("Scan(%s) errors = %v", root, res.Errors)
	}

	return res, analyzer.Analyze(res)
}

func statuses(report *analyzer.Report) map[string]analyzer.Status {
	out := make(map[string]analyzer.Status, len(report.Variables))
	for _, v := range report.Variables {
		out[v.Name] = v.Status
	}
	return out
}

func assertStatuses(t *testing.T, report *analyzer.Report, want map[string]analyzer.Status) {
	t.Helper()

	got := statuses(report)
	if len(got) != len(want) {
		t.Errorf("found variables %v, want %v", got, want)
	}
	for name, status := range want {
		if got[name] != status {
			t.Errorf("%s = %q, want %q", name, got[name], status)
		}
	}
}

func TestSimpleGoProject(t *testing.T) {
	res, report := analyze(t, "simple-go")

	assertStatuses(t, report, map[string]analyzer.Status{
		"DATABASE_URL": analyzer.StatusOK,
		"REDIS_HOST":   analyzer.StatusOK,
		"PORT":         analyzer.StatusOK,
		"LOG_LEVEL":    analyzer.StatusOK,
		"QUEUE_NAME":   analyzer.StatusOK,
		"JWT_SECRET":   analyzer.StatusMissing,
		"OLD_API_KEY":  analyzer.StatusUnused,
	})

	var db analyzer.Variable
	for _, v := range report.Variables {
		if v.Name == "DATABASE_URL" {
			db = v
		}
	}

	if len(db.Sources) != 1 || db.Sources[0].Location.File != ".env" {
		t.Errorf("DATABASE_URL sources = %+v, want one in .env", db.Sources)
	}
	if len(db.PassedTo) != 2 {
		t.Errorf("DATABASE_URL passedTo = %+v, want api and worker", db.PassedTo)
	}
	if len(db.Consumers) != 1 || db.Consumers[0].File != "config/database.go" {
		t.Errorf("DATABASE_URL consumers = %+v, want config/database.go", db.Consumers)
	}

	if len(res.Services) != 2 {
		t.Errorf("services = %+v, want api and worker", res.Services)
	}
}

func TestPortResolvesThroughItsComposeDefault(t *testing.T) {
	_, report := analyze(t, "simple-go")

	for _, v := range report.Variables {
		if v.Name != "PORT" {
			continue
		}
		// One source in .env, one from the ${PORT:-8080} fallback.
		if len(v.Sources) != 2 {
			t.Errorf("PORT sources = %+v, want the .env entry and the compose default", v.Sources)
		}
	}
}

func TestComposePythonProject(t *testing.T) {
	_, report := analyze(t, "compose-python")

	assertStatuses(t, report, map[string]analyzer.Status{
		"POSTGRES_HOST":     analyzer.StatusOK,
		"POSTGRES_PASSWORD": analyzer.StatusOK,
		"POSTGRES_DB":       analyzer.StatusOK,
		"S3_BUCKET":         analyzer.StatusOK,
		"WORKERS":           analyzer.StatusOK,
		"DB_HOST":           analyzer.StatusOK, // renamed from POSTGRES_HOST
		"SENTRY_DSN":        analyzer.StatusMissing,
	})

	// S3_BUCKET reaches the web container through env_file, not an explicit environment entry.
	for _, v := range report.Variables {
		if v.Name != "S3_BUCKET" {
			continue
		}
		if len(v.PassedTo) != 1 || v.PassedTo[0].Service != "web" {
			t.Errorf("S3_BUCKET passedTo = %+v, want web via env_file", v.PassedTo)
		}
	}
}

func TestGraphIsWellFormed(t *testing.T) {
	for _, example := range []string{"simple-go", "compose-python"} {
		t.Run(example, func(t *testing.T) {
			res, report := analyze(t, example)
			g := analyzer.Graph(res, report)

			nodes := make(map[string]bool)
			for _, n := range g.Nodes() {
				nodes[n.ID] = true
			}

			for _, v := range report.Variables {
				if !nodes["var:"+v.Name] {
					t.Errorf("variable %s is missing from the graph", v.Name)
				}
			}

			for _, e := range g.Edges() {
				if !nodes[e.From] {
					t.Errorf("edge %+v starts at an unknown node", e)
				}
				if !nodes[e.To] {
					t.Errorf("edge %+v ends at an unknown node", e)
				}
			}
		})
	}
}

func run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestCheckExitCodesOnTheExamples(t *testing.T) {
	for _, example := range []string{"simple-go", "compose-python"} {
		t.Run(example, func(t *testing.T) {
			code, stdout, stderr := run("check", examplePath(example))
			if code != 1 {
				t.Errorf("exit code = %d, want 1: both examples have a missing variable", code)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want nothing", stderr)
			}
			if !strings.Contains(stdout, "ERROR") {
				t.Errorf("output has no findings:\n%s", stdout)
			}
		})
	}
}

func TestExportedGraphParsesForEveryExample(t *testing.T) {
	for _, example := range []string{"simple-go", "compose-python"} {
		t.Run(example, func(t *testing.T) {
			code, stdout, _ := run("export", examplePath(example), "-o", "-")
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}

			var doc struct {
				Nodes []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"nodes"`
				Edges []struct {
					From         string `json:"from"`
					To           string `json:"to"`
					Relationship string `json:"relationship"`
				} `json:"edges"`
			}
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
				t.Fatalf("export is not valid JSON: %v", err)
			}

			kinds := make(map[string]bool)
			for _, n := range doc.Nodes {
				kinds[n.Type] = true
			}
			for _, want := range []string{"file", "service", "variable"} {
				if !kinds[want] {
					t.Errorf("graph has no %s nodes", want)
				}
			}

			rels := make(map[string]bool)
			for _, e := range doc.Edges {
				rels[e.Relationship] = true
			}
			for _, want := range []string{"defines", "declares", "passed_to", "consumed_by"} {
				if !rels[want] {
					t.Errorf("graph has no %s edges", want)
				}
			}
		})
	}
}

// TestViewerServesTheExamples drives the real handler over HTTP, which is
// the only place the embedded assets and the API meet.
func TestViewerServesTheExamples(t *testing.T) {
	for _, example := range []string{"simple-go", "compose-python"} {
		t.Run(example, func(t *testing.T) {
			ts := httptest.NewServer(server.New(server.Options{Root: examplePath(example)}))
			defer ts.Close()

			body := getBody(t, ts.URL+"/api/graph")

			var doc struct {
				Files     []struct{} `json:"files"`
				Variables []struct {
					Name    string `json:"name"`
					Status  string `json:"status"`
					Sources []struct {
						Value string `json:"value"`
					} `json:"sources"`
				} `json:"variables"`
				Graph struct {
					Nodes []struct{} `json:"nodes"`
					Edges []struct{} `json:"edges"`
				} `json:"graph"`
			}
			if err := json.Unmarshal(body, &doc); err != nil {
				t.Fatalf("api/graph is not valid JSON: %v", err)
			}

			if len(doc.Variables) == 0 || len(doc.Graph.Nodes) == 0 {
				t.Fatalf("document is empty: %s", body)
			}

			for _, v := range doc.Variables {
				for _, s := range v.Sources {
					if s.Value != "" {
						t.Errorf("%s leaked the value %q to the browser", v.Name, s.Value)
					}
				}
			}

			// The page and every asset it references must load.
			page := string(getBody(t, ts.URL+"/"))
			for _, asset := range []string{"/app.js", "/style.css", "/vendor/cytoscape.min.js"} {
				if !strings.Contains(page, strings.TrimPrefix(asset, "/")) {
					t.Errorf("the page does not reference %s", asset)
				}
				getBody(t, ts.URL+asset)
			}
		})
	}
}

func getBody(t *testing.T, url string) []byte {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if len(body) == 0 {
		t.Fatalf("GET %s returned an empty body", url)
	}
	return body
}
