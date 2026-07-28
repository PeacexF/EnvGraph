// Package analyzer turns raw parser occurrences into a variable report and the graph that visualises it
package analyzer

import (
	"sort"

	"github.com/PeacexF/EnvGraph/internal/graph"
	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/scanner"
)

// Status is the verdict for a variable.
type Status string

const (
	StatusOK      Status = "ok"
	StatusMissing Status = "missing"
	StatusUnused  Status = "unused"
)

// Source is a place that supplies a variable's value.
type Source struct {
	Location parser.Location `json:"location"`

	// FromDefault marks a compose fallback such as ${PORT:-8080}.
	FromDefault bool `json:"fromDefault,omitempty"`

	// DerivedFrom names the variables this value was built from.
	DerivedFrom []string `json:"derivedFrom,omitempty"`

	// Value is kept for comparison work; the CLI hides it unless asked,
	// because .env files hold credentials.
	Value string `json:"value,omitempty"`
}

// Injection is a service that receives a variable.
type Injection struct {
	Service  string          `json:"service"`
	Location parser.Location `json:"location"`
}

// Variable is everything known about one environment variable.
type Variable struct {
	Name      string            `json:"name"`
	Status    Status            `json:"status"`
	Sources   []Source          `json:"sources,omitempty"`
	PassedTo  []Injection       `json:"passedTo,omitempty"`
	Consumers []parser.Location `json:"consumers,omitempty"`
}

// Report is the full analysis of a scanned project.
type Report struct {
	Variables []Variable `json:"variables"`
}

// Analyze folds a scan result into a report
func Analyze(res *scanner.Result) *Report {
	vars := make(map[string]*Variable)

	get := func(name string) *Variable {
		v, ok := vars[name]
		if !ok {
			v = &Variable{Name: name}
			vars[name] = v
		}
		return v
	}

	var derivations []parser.Occurrence

	for _, occ := range res.Occurrences {
		v := get(occ.Name)

		switch occ.Kind {
		case parser.KindDefinition:
			v.Sources = append(v.Sources, Source{Location: occ.Location, Value: occ.Value})
		case parser.KindReference:
			switch {
			case occ.HasDefault:
				v.Sources = append(v.Sources, Source{Location: occ.Location, FromDefault: true})
			case len(occ.DerivedFrom) > 0:
				// Held back until every variable is known.
				derivations = append(derivations, occ)
			}

			if occ.Service == "" {
				v.Consumers = append(v.Consumers, occ.Location)
			}
		case parser.KindConsumption:
			v.Consumers = append(v.Consumers, occ.Location)
		}

		if occ.Service != "" {
			v.PassedTo = append(v.PassedTo, Injection{
				Service:  occ.Service,
				Location: occ.Location,
			})
		}
	}

	// A service that loads an env file receives every variable in it.
	for _, svc := range res.Services {
		for _, envFile := range svc.EnvFiles {
			for _, occ := range res.Occurrences {
				if occ.Location.File != envFile || occ.Kind != parser.KindDefinition {
					continue
				}
				v := get(occ.Name)
				v.PassedTo = append(v.PassedTo, Injection{
					Service:  svc.Name,
					Location: svc.Location,
				})
			}
		}
	}

	resolveDerived(vars, derivations)

	report := &Report{Variables: make([]Variable, 0, len(vars))}
	for _, v := range vars {
		v.Status = status(v)
		dedupe(v)
		report.Variables = append(report.Variables, *v)
	}
	sort.Slice(report.Variables, func(i, j int) bool {
		return report.Variables[i].Name < report.Variables[j].Name
	})

	return report
}

// resolveDerived settles variables built from other variables
func resolveDerived(vars map[string]*Variable, derivations []parser.Occurrence) {
	pending := derivations

	for len(pending) > 0 {
		var stuck []parser.Occurrence

		for _, occ := range pending {
			if !allSupplied(vars, occ.DerivedFrom) {
				stuck = append(stuck, occ)
				continue
			}
			v := vars[occ.Name]
			v.Sources = append(v.Sources, Source{
				Location:    occ.Location,
				DerivedFrom: occ.DerivedFrom,
			})
		}

		if len(stuck) == len(pending) {
			return
		}
		pending = stuck
	}
}

func allSupplied(vars map[string]*Variable, names []string) bool {
	for _, name := range names {
		v, ok := vars[name]
		if !ok || len(v.Sources) == 0 {
			return false
		}
	}
	return true
}

func status(v *Variable) Status {
	switch {
	case len(v.Sources) == 0:
		return StatusMissing
	case len(v.Consumers) == 0 && len(v.PassedTo) == 0:
		return StatusUnused
	default:
		return StatusOK
	}
}

// Missing returns the variables nothing supplies.
func (r *Report) Missing() []Variable { return r.filter(StatusMissing) }

// Unused returns the variables nothing reads.
func (r *Report) Unused() []Variable { return r.filter(StatusUnused) }

func (r *Report) filter(s Status) []Variable {
	var out []Variable
	for _, v := range r.Variables {
		if v.Status == s {
			out = append(out, v)
		}
	}
	return out
}

// Graph renders a report and its scan as nodes and edges.
func Graph(res *scanner.Result, report *Report) *graph.Graph {
	g := graph.New()

	fileTypes := make(map[string]scanner.FileType, len(res.Files))
	for _, f := range res.Files {
		fileTypes[f.Path] = f.Type
	}

	// Files are added on demand, so source files that never touch configuration stay out of the graph.
	addFile := func(path string) string {
		id := graph.FileID(path)
		g.AddNode(graph.Node{
			ID:       id,
			Type:     graph.NodeFile,
			Name:     path,
			File:     path,
			Category: string(fileTypes[path]),
		})
		return id
	}

	serviceIDs := make(map[string]string, len(res.Services))
	for _, svc := range res.Services {
		composeFile := svc.Location.File
		id := graph.ServiceID(composeFile, svc.Name)
		serviceIDs[composeFile+":"+svc.Name] = id

		g.AddNode(graph.Node{
			ID:   id,
			Type: graph.NodeService,
			Name: svc.Name,
			File: composeFile,
			Line: svc.Location.Line,
		})
		g.AddEdge(graph.Edge{
			From:         addFile(composeFile),
			To:           id,
			Relationship: graph.RelDeclares,
			File:         composeFile,
			Line:         svc.Location.Line,
		})
	}

	// Fallback for a service declared in another file, such as a compose override. A name declared twice is ambiguous and gets dropped.
	byName := make(map[string]string, len(res.Services))
	for _, svc := range res.Services {
		if _, clash := byName[svc.Name]; clash {
			byName[svc.Name] = ""
			continue
		}
		byName[svc.Name] = graph.ServiceID(svc.Location.File, svc.Name)
	}

	serviceID := func(inj Injection) (string, bool) {
		if id, ok := serviceIDs[inj.Location.File+":"+inj.Service]; ok {
			return id, true
		}
		id := byName[inj.Service]
		return id, id != ""
	}

	for _, v := range report.Variables {
		varID := graph.VariableID(v.Name)
		g.AddNode(graph.Node{
			ID:     varID,
			Type:   graph.NodeVariable,
			Name:   v.Name,
			Status: string(v.Status),
		})

		for _, src := range v.Sources {
			g.AddEdge(graph.Edge{
				From:         addFile(src.Location.File),
				To:           varID,
				Relationship: graph.RelDefines,
				File:         src.Location.File,
				Line:         src.Location.Line,
			})
		}

		for _, inj := range v.PassedTo {
			id, ok := serviceID(inj)
			if !ok {
				continue
			}
			g.AddEdge(graph.Edge{
				From:         varID,
				To:           id,
				Relationship: graph.RelPassedTo,
				File:         inj.Location.File,
				Line:         inj.Location.Line,
			})
		}

		for _, loc := range v.Consumers {
			g.AddEdge(graph.Edge{
				From:         varID,
				To:           addFile(loc.File),
				Relationship: graph.RelConsumedBy,
				File:         loc.File,
				Line:         loc.Line,
			})
		}
	}

	return g
}

// dedupe removes repeats, which arise when a variable is written in two places that resolve to the same location.
func dedupe(v *Variable) {
	sort.Slice(v.Sources, func(i, j int) bool { return less(v.Sources[i].Location, v.Sources[j].Location) })
	sort.Slice(v.Consumers, func(i, j int) bool { return less(v.Consumers[i], v.Consumers[j]) })
	sort.Slice(v.PassedTo, func(i, j int) bool {
		if v.PassedTo[i].Service != v.PassedTo[j].Service {
			return v.PassedTo[i].Service < v.PassedTo[j].Service
		}
		return less(v.PassedTo[i].Location, v.PassedTo[j].Location)
	})

	v.Sources = compact(v.Sources, func(a, b Source) bool { return a.Location == b.Location })
	v.Consumers = compact(v.Consumers, func(a, b parser.Location) bool { return a == b })
	v.PassedTo = compact(v.PassedTo, func(a, b Injection) bool { return a == b })
}

func compact[T any](s []T, equal func(a, b T) bool) []T {
	if len(s) < 2 {
		return s
	}
	out := s[:1]
	for _, item := range s[1:] {
		if !equal(out[len(out)-1], item) {
			out = append(out, item)
		}
	}
	return out
}

func less(a, b parser.Location) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	return a.Line < b.Line
}

// Without drops the variables an ignore rule matches
func (r *Report) Without(ignored func(name string) bool) *Report {
	if ignored == nil {
		return r
	}

	out := &Report{Variables: make([]Variable, 0, len(r.Variables))}
	for _, v := range r.Variables {
		if !ignored(v.Name) {
			out.Variables = append(out.Variables, v)
		}
	}
	return out
}
