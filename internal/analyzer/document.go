package analyzer

import (
	"github.com/PeacexF/EnvGraph/internal/graph"
	"github.com/PeacexF/EnvGraph/internal/scanner"
)

type Document struct {
	Files     []scanner.File `json:"files"`
	Variables []Variable     `json:"variables"`
	Graph     *graph.Graph   `json:"graph"`
}

// NewDocument builds the payload for a scan.
func NewDocument(res *scanner.Result, report *Report, withValues bool) Document {
	if !withValues {
		report = report.Redacted()
	}

	return Document{
		Files:     res.Files,
		Variables: report.Variables,
		Graph:     Graph(res, report),
	}
}

// Redacted returns a copy of the report with assigned values stripped.
func (r *Report) Redacted() *Report {
	out := &Report{Variables: make([]Variable, len(r.Variables))}

	for i, v := range r.Variables {
		v.Sources = append([]Source(nil), v.Sources...)
		for j := range v.Sources {
			v.Sources[j].Value = ""
		}
		out.Variables[i] = v
	}

	return out
}
