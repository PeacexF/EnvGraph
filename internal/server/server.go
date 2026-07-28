package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
	"github.com/PeacexF/EnvGraph/internal/scanner"
	"github.com/PeacexF/EnvGraph/web"
)

// Options configures a viewer.
type Options struct {
	// Root is the project to scan.
	Root string

	// Scan is passed through to the scanner on every request.
	Scan scanner.Options

	// ShowValues serves the value assigned to each variable. Off by default: .env files hold credentials, and this is an HTTP endpoint.
	ShowValues bool

	// Assets overrides the embedded viewer, for tests.
	Assets fs.FS
}

type server struct {
	opts Options
	mux  *http.ServeMux
}

// New returns a handler serving the viewer and its API
func New(opts Options) http.Handler {
	if opts.Assets == nil {
		opts.Assets = web.FS()
	}
	if opts.Root == "" {
		opts.Root = "."
	}

	s := &server{opts: opts, mux: http.NewServeMux()}

	s.mux.HandleFunc("GET /api/graph", s.handleGraph)
	s.mux.HandleFunc("GET /api/meta", s.handleMeta)
	s.mux.Handle("GET /", http.FileServerFS(opts.Assets))

	return s.mux
}

func (s *server) handleGraph(w http.ResponseWriter, r *http.Request) {
	res, err := scanner.Scan(s.opts.Root, s.opts.Scan)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}

	report := analyzer.Analyze(res)
	writeJSON(w, analyzer.NewDocument(res, report, s.opts.ShowValues))
}

func (s *server) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"root":       s.opts.Root,
		"showValues": s.opts.ShowValues,
	})
}

func writeJSON(w http.ResponseWriter, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// The graph is rebuilt per request and may change between them.
	w.Header().Set("Cache-Control", "no-store")

	w.Write(body)
}

func httpError(w http.ResponseWriter, code int, err error) {
	http.Error(w, fmt.Sprintf("envgraph: %v", err), code)
}
