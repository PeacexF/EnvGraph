// Package workflow parses GitHub Actions workflows
package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/interpolate"
)

// Origins name the external providers a workflow can draw a value from.
const (
	OriginSecret = "github-secret"
	OriginVar    = "github-var"
	OriginInput  = "github-input"
)

// An expression may hold operators and several references, as in:
// "${{ env.MODE == 'live' && secrets.LIVE_KEY || secrets.TEST_KEY }}"
// the block is matched first and the references are read from inside it.
var (
	expressionBlock = regexp.MustCompile(`(?s)\$\{\{(.*?)\}\}`)

	// Only the contexts that carry configuration are listed; github.*, runner.* and matrix.* are not environment variables.
	contextRef = regexp.MustCompile(`\b(secrets|vars|inputs|env)\.([A-Za-z_][A-Za-z0-9_-]*)`)
)

// reference is one context.NAME mention inside an expression.
type reference struct{ context, name string }

// expressions returns every configuration reference in a scalar.
func expressions(s string) []reference {
	var out []reference

	for _, block := range expressionBlock.FindAllStringSubmatch(s, -1) {
		for _, m := range contextRef.FindAllStringSubmatch(block[1], -1) {
			out = append(out, reference{context: m[1], name: m[2]})
		}
	}

	return out
}

// assignment and loopVar find names a script creates for itself
var (
	assignment = regexp.MustCompile(`(?m)(?:^|[;&|]|\bexport\s+|\blocal\s+|\bdeclare\s+)\s*([A-Za-z_][A-Za-z0-9_]*)=`)
	loopVar    = regexp.MustCompile(`\bfor\s+([A-Za-z_][A-Za-z0-9_]*)\s+in\b`)
	readVar    = regexp.MustCompile(`\bread\s+(?:-[A-Za-z]+\s+)*([A-Za-z_][A-Za-z0-9_]*)`)
)

// locals collects the names a script assigns to itself.
func locals(script string) map[string]bool {
	out := make(map[string]bool)

	for _, re := range []*regexp.Regexp{assignment, loopVar, readVar} {
		for _, m := range re.FindAllStringSubmatch(script, -1) {
			out[m[1]] = true
		}
	}

	return out
}

// Parse reads a GitHub Actions workflow
func Parse(filePath string, content []byte) (parser.Result, error) {
	var res parser.Result

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return res, fmt.Errorf("parse yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return res, nil
	}
	doc := root.Content[0]

	// Workflow-level env belongs to the file rather than to any one job.
	res.Occurrences = append(res.Occurrences, envBlock(doc, filePath, "")...)

	jobs := mapValue(doc, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return res, nil
	}

	for i := 0; i+1 < len(jobs.Content); i += 2 {
		nameNode, body := jobs.Content[i], jobs.Content[i+1]
		if body.Kind != yaml.MappingNode {
			continue
		}

		job := nameNode.Value
		res.Services = append(res.Services, parser.Service{
			Name:     job,
			Location: parser.Location{File: filePath, Line: nameNode.Line},
		})

		res.Occurrences = append(res.Occurrences, envBlock(body, filePath, job)...)
		res.Occurrences = append(res.Occurrences, steps(body, filePath, job)...)
		res.Occurrences = append(res.Occurrences, reads(body, filePath, job)...)
	}

	return res, nil
}

// reads finds every ${{ env.X }} in a subtree
func reads(node *yaml.Node, filePath, jobName string) []parser.Occurrence {
	var out []parser.Occurrence

	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}

		if n.Kind == yaml.ScalarNode {
			for _, ref := range expressions(n.Value) {
				if ref.context != "env" {
					continue
				}
				out = append(out, parser.Occurrence{
					Name:     ref.name,
					Kind:     parser.KindConsumption,
					Location: lineOf(filePath, n),
					Service:  jobName,
				})
			}
			return
		}

		for _, child := range n.Content {
			walk(child)
		}
	}

	walk(node)
	return out
}

// steps walks a job's steps for their env blocks and shell scripts.
func steps(job *yaml.Node, filePath, jobName string) []parser.Occurrence {
	list := mapValue(job, "steps")
	if list == nil || list.Kind != yaml.SequenceNode {
		return nil
	}

	var out []parser.Occurrence

	for _, step := range list.Content {
		if step.Kind != yaml.MappingNode {
			continue
		}

		out = append(out, envBlock(step, filePath, jobName)...)

		if run := mapValue(step, "run"); run != nil && run.Kind == yaml.ScalarNode {
			out = append(out, script(run, filePath, jobName)...)
		}
	}

	return out
}

// script finds the shell variables a run block reads. This is where a missing workflow variable actually bites, so it is worth reading the shell.
func script(run *yaml.Node, filePath, jobName string) []parser.Occurrence {
	// GitHub substitutes ${{ ... }} before the shell ever sees it, so those are handled separately and must not be read as shell syntax.
	shell := expressionBlock.ReplaceAllString(run.Value, "")

	var out []parser.Occurrence
	seen := locals(shell)

	for _, ref := range interpolate.Find(shell) {
		if seen[ref.Name] {
			continue
		}
		seen[ref.Name] = true

		out = append(out, parser.Occurrence{
			Name:     ref.Name,
			Kind:     parser.KindConsumption,
			Location: lineOf(filePath, run),
			Service:  jobName,
		})
	}

	return out
}

// envBlock converts an "env:" mapping into occurrences.
func envBlock(node *yaml.Node, filePath, jobName string) []parser.Occurrence {
	env := mapValue(node, "env")
	if env == nil || env.Kind != yaml.MappingNode {
		return nil
	}

	var out []parser.Occurrence

	for i := 0; i+1 < len(env.Content); i += 2 {
		key, value := env.Content[i], env.Content[i+1]
		if value.Kind != yaml.ScalarNode {
			continue
		}
		out = append(out, entry(key.Value, value.Value, lineOf(filePath, key), jobName)...)
	}

	return out
}

// entry turns one env pair into the occurrences it implies.
func entry(key, value string, loc parser.Location, jobName string) []parser.Occurrence {
	if key == "" {
		return nil
	}

	matches := expressions(value)
	if len(matches) == 0 {
		return []parser.Occurrence{{
			Name:     key,
			Kind:     parser.KindDefinition,
			Value:    value,
			Location: loc,
			Service:  jobName,
		}}
	}

	var out []parser.Occurrence
	var origin string
	var readsEnv []string

	for _, m := range matches {
		switch m.context {
		case "secrets":
			origin = OriginSecret
		case "vars":
			origin = OriginVar
		case "inputs":
			origin = OriginInput
		case "env":
			// The read itself is recorded by the document-wide walk
			readsEnv = append(readsEnv, m.name)
		}
	}

	// A secret, a repository variable, or a workflow input all supply the value from outside the repository, so the key is genuinely provided.
	if origin != "" {
		out = append(out, parser.Occurrence{
			Name:     key,
			Kind:     parser.KindDefinition,
			Location: loc,
			Service:  jobName,
			Origin:   origin,
		})
		return out
	}

	// Otherwise the key is only as available as the variables it reads.
	out = append(out, parser.Occurrence{
		Name:        key,
		Kind:        parser.KindReference,
		Location:    loc,
		Service:     jobName,
		DerivedFrom: readsEnv,
	})

	return out
}

func lineOf(filePath string, node *yaml.Node) parser.Location {
	return parser.Location{File: filePath, Line: node.Line}
}

// mapValue returns the value node for key in a mapping, or nil.
func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if strings.EqualFold(node.Content[i].Value, key) {
			return node.Content[i+1]
		}
	}
	return nil
}
