package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PeacexF/EnvGraph/internal/scanner"
	"github.com/PeacexF/EnvGraph/internal/server"
)

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

func sample(t *testing.T) string {
	t.Helper()

	return project(t, map[string]string{
		".env": "DATABASE_URL=postgres://localhost\nTOKEN=s3cret\n",
		"docker-compose.yml": "services:\n  api:\n    environment:\n" +
			"      DATABASE_URL: ${DATABASE_URL}\n",
		"main.go": "package main\nimport \"os\"\nfunc main() { _ = os.Getenv(\"DATABASE_URL\") }\n",
	})
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

type document struct {
	Files []struct {
		Path string `json:"path"`
	} `json:"files"`
	Variables []struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Sources []struct {
			Value string `json:"value"`
		} `json:"sources"`
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

func graphOf(t *testing.T, handler http.Handler) document {
	t.Helper()

	rec := get(t, handler, "/api/graph")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var doc document
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body is not valid JSON: %v\n%s", err, rec.Body)
	}
	return doc
}

func TestGraphEndpoint(t *testing.T) {
	handler := server.New(server.Options{Root: sample(t)})
	doc := graphOf(t, handler)

	if len(doc.Files) != 3 {
		t.Errorf("files = %+v, want three", doc.Files)
	}
	if len(doc.Variables) != 2 {
		t.Errorf("variables = %+v, want two", doc.Variables)
	}
	if len(doc.Graph.Nodes) == 0 || len(doc.Graph.Edges) == 0 {
		t.Errorf("graph = %+v, want nodes and edges", doc.Graph)
	}
}

func TestValuesAreRedactedByDefault(t *testing.T) {
	handler := server.New(server.Options{Root: sample(t)})

	if body := get(t, handler, "/api/graph").Body.String(); strings.Contains(body, "s3cret") {
		t.Errorf("the API served a secret without being asked:\n%s", body)
	}
}

func TestShowValuesOptOut(t *testing.T) {
	handler := server.New(server.Options{Root: sample(t), ShowValues: true})

	if body := get(t, handler, "/api/graph").Body.String(); !strings.Contains(body, "s3cret") {
		t.Errorf("ShowValues did not include the value:\n%s", body)
	}
}

func TestGraphReflectsEditsWithoutRestarting(t *testing.T) {
	root := project(t, map[string]string{".env": "FIRST=1\n"})
	handler := server.New(server.Options{Root: root})

	if got := len(graphOf(t, handler).Variables); got != 1 {
		t.Fatalf("variables = %d, want 1", got)
	}

	// The point of re-scanning per request: an edit shows up on refresh.
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("FIRST=1\nSECOND=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := len(graphOf(t, handler).Variables); got != 2 {
		t.Errorf("variables = %d after the edit, want 2", got)
	}
}

func TestScanOptionsArePassedThrough(t *testing.T) {
	root := project(t, map[string]string{
		".env":          "KEPT=1\n",
		"fixtures/.env": "DROPPED=2\n",
	})

	handler := server.New(server.Options{
		Root: root,
		Scan: scanner.Options{Exclude: []string{"fixtures"}},
	})

	for _, v := range graphOf(t, handler).Variables {
		if v.Name == "DROPPED" {
			t.Error("the excluded directory was scanned anyway")
		}
	}
}

func TestGraphOnAMissingProject(t *testing.T) {
	handler := server.New(server.Options{Root: filepath.Join(t.TempDir(), "nowhere")})

	if rec := get(t, handler, "/api/graph"); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestGraphOnAnEmptyProject(t *testing.T) {
	handler := server.New(server.Options{Root: t.TempDir()})
	doc := graphOf(t, handler)

	if len(doc.Variables) != 0 || len(doc.Graph.Nodes) != 0 {
		t.Errorf("document = %+v, want empty", doc)
	}
}

func TestMetaEndpoint(t *testing.T) {
	root := sample(t)
	handler := server.New(server.Options{Root: root})

	rec := get(t, handler, "/api/meta")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var meta struct {
		Root       string `json:"root"`
		ShowValues bool   `json:"showValues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if meta.Root != root {
		t.Errorf("root = %q, want %q", meta.Root, root)
	}
	if meta.ShowValues {
		t.Error("showValues = true, want false by default")
	}
}

func TestAPIResponsesAreNotCached(t *testing.T) {
	handler := server.New(server.Options{Root: sample(t)})

	for _, path := range []string{"/api/graph", "/api/meta"} {
		rec := get(t, handler, path)
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want no-store: the graph changes between requests", path, got)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("%s Content-Type = %q, want application/json", path, got)
		}
	}
}

func TestStaticAssetsAreServed(t *testing.T) {
	handler := server.New(server.Options{Root: sample(t)})

	for _, path := range []string{"/", "/app.js", "/style.css", "/vendor/cytoscape.min.js"} {
		rec := get(t, handler, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s served an empty body", path)
		}
	}
}

func TestIndexHTMLRedirectsToRoot(t *testing.T) {
	// net/http canonicalizes /index.html to /; assert it rather than
	// letting a plain 200-check hide the redirect.
	handler := server.New(server.Options{Root: sample(t)})

	rec := get(t, handler, "/index.html")
	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "./" {
		t.Errorf("Location = %q, want ./", got)
	}
}

func TestServedIndexIsTheViewer(t *testing.T) {
	handler := server.New(server.Options{Root: sample(t)})

	body := get(t, handler, "/").Body.String()
	for _, want := range []string{"<title>EnvGraph</title>", "app.js", "cytoscape.min.js", `id="graph"`} {
		if !strings.Contains(body, want) {
			t.Errorf("index does not reference %q", want)
		}
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	handler := server.New(server.Options{Root: sample(t)})

	if rec := get(t, handler, "/nope.js"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestPostIsRejected(t *testing.T) {
	handler := server.New(server.Options{Root: sample(t)})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/graph", nil))

	if rec.Code == http.StatusOK {
		t.Error("POST to the API succeeded, want it rejected")
	}
}

func TestRootDefaultsToTheWorkingDirectory(t *testing.T) {
	root := project(t, map[string]string{".env": "HERE=1\n"})
	t.Chdir(root)

	handler := server.New(server.Options{})
	doc := graphOf(t, handler)

	if len(doc.Variables) != 1 || doc.Variables[0].Name != "HERE" {
		t.Errorf("variables = %+v, want HERE from the working directory", doc.Variables)
	}
}
