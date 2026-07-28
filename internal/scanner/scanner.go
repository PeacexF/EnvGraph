package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/compose"
	"github.com/PeacexF/EnvGraph/internal/parser/env"
	"github.com/PeacexF/EnvGraph/internal/parser/golang"
	"github.com/PeacexF/EnvGraph/internal/parser/python"
)

// FileType is the role a scanned file plays.
type FileType string

const (
	TypeEnv     FileType = "env"
	TypeCompose FileType = "compose"
	TypeGo      FileType = "go"
	TypePython  FileType = "python"
)

// maxFileSize skips anything too large to be hand-written configuration.
const maxFileSize = 2 << 20 // 2 MiB

var skipDirs = map[string]bool{
	".git":          true,
	".hg":           true,
	".svn":          true,
	".idea":         true,
	".vscode":       true,
	"node_modules":  true,
	"vendor":        true,
	"venv":          true,
	".venv":         true,
	"__pycache__":   true,
	"dist":          true,
	"build":         true,
	"target":        true,
	".next":         true,
	".tox":          true,
	".mypy_cache":   true,
	".pytest_cache": true,
}

// File is one file the scanner recognised.
type File struct {
	Path string   `json:"path"`
	Type FileType `json:"type"`
}

// Result is everything a scan found. Errors holds files that were recognised but could not be parsed; one bad file does not abort the scan.
type Result struct {
	Root        string
	Files       []File
	Occurrences []parser.Occurrence
	Services    []parser.Service
	Errors      []error
}

// Options tunes a scan.
type Options struct {
	Exclude      []string
	IncludeTests bool
}

// Scan walks root and parses every file it recognises.
func Scan(root string, opts Options) (*Result, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", root, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	excluded := make(map[string]bool, len(opts.Exclude))
	for _, name := range opts.Exclude {
		excluded[name] = true
	}

	res := &Result{Root: abs}

	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			res.Errors = append(res.Errors, err)
			return nil
		}

		if d.IsDir() {
			if path == abs {
				return nil
			}
			if skipDirs[d.Name()] || excluded[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		fileType, ok := classify(d.Name())
		if !ok {
			return nil
		}
		if !opts.IncludeTests && isTest(fileType, d.Name()) {
			return nil
		}

		if info, err := d.Info(); err == nil && info.Size() > maxFileSize {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			res.Errors = append(res.Errors, err)
			return nil
		}

		parsed, err := parse(fileType, rel, content)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", rel, err))
			return nil
		}

		res.Files = append(res.Files, File{Path: rel, Type: fileType})
		res.Occurrences = append(res.Occurrences, parsed.Occurrences...)
		res.Services = append(res.Services, parsed.Services...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })

	return res, nil
}

func parse(t FileType, rel string, content []byte) (parser.Result, error) {
	switch t {
	case TypeEnv:
		return env.Parse(rel, content)
	case TypeCompose:
		return compose.Parse(rel, content)
	case TypeGo:
		return golang.Parse(rel, content)
	case TypePython:
		return python.Parse(rel, content)
	default:
		return parser.Result{}, nil
	}
}

func classify(name string) (FileType, bool) {
	switch {
	case isEnvFile(name):
		return TypeEnv, true
	case isComposeFile(name):
		return TypeCompose, true
	case strings.HasSuffix(name, ".go"):
		return TypeGo, true
	case strings.HasSuffix(name, ".py"):
		return TypePython, true
	}
	return "", false
}

// isEnvFile matches .env, .env.local, and prod.env. Templates such as .env.example count too: they document what a project expects.
func isEnvFile(name string) bool {
	return name == ".env" || strings.HasPrefix(name, ".env.") || strings.HasSuffix(name, ".env")
}

func isComposeFile(name string) bool {
	ext := filepath.Ext(name)
	if ext != ".yml" && ext != ".yaml" {
		return false
	}
	base := strings.TrimSuffix(name, ext)
	return base == "compose" || base == "docker-compose" ||
		strings.HasPrefix(base, "compose.") || strings.HasPrefix(base, "docker-compose.")
}

func isTest(t FileType, name string) bool {
	switch t {
	case TypeGo:
		return strings.HasSuffix(name, "_test.go")
	case TypePython:
		return strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.py")
	}
	return false
}
