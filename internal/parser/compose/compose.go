// Package compose parses Docker Compose files into services and the environment variables that flow into them.
package compose

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/interpolate"
)

// Parse reads a compose file. filePath must be relative to the scan root, so that env_file entries resolve to the same paths the scanner reports.
// A compose file mostly passes values along: "DATABASE_URL: ${DATABASE_URL}" supplies nothing. Only a literal, or a reference with a fallback, does.
func Parse(filePath string, content []byte) (parser.Result, error) {
	var res parser.Result

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return res, fmt.Errorf("parse yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return res, nil
	}

	services := mapValue(root.Content[0], "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return res, nil
	}

	dir := path.Dir(filePath)

	for i := 0; i+1 < len(services.Content); i += 2 {
		nameNode, body := services.Content[i], services.Content[i+1]
		if body.Kind != yaml.MappingNode {
			continue
		}

		svc := parser.Service{
			Name:     nameNode.Value,
			Location: parser.Location{File: filePath, Line: nameNode.Line},
			EnvFiles: envFiles(body, dir),
		}
		res.Services = append(res.Services, svc)

		res.Occurrences = append(res.Occurrences,
			environment(body, filePath, svc.Name)...)
	}

	return res, nil
}

func environment(service *yaml.Node, filePath, serviceName string) []parser.Occurrence {
	env := mapValue(service, "environment")
	if env == nil {
		return nil
	}

	var out []parser.Occurrence

	switch env.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(env.Content); i += 2 {
			k, v := env.Content[i], env.Content[i+1]
			if v.Kind != yaml.ScalarNode {
				continue
			}
			out = append(out, entry(k.Value, v.Value, true, filePath, k.Line, serviceName)...)
		}

	case yaml.SequenceNode:
		for _, item := range env.Content {
			if item.Kind != yaml.ScalarNode {
				continue
			}
			key, value, assigned := strings.Cut(item.Value, "=")
			out = append(out, entry(strings.TrimSpace(key), value, assigned,
				filePath, item.Line, serviceName)...)
		}
	}

	return out
}

// entry turns one KEY/value pair into the occurrences it implies. assigned separates "- KEY=value" from the bare "- KEY", which supplies nothing.
func entry(key, value string, assigned bool, filePath string, line int, service string) []parser.Occurrence {
	if key == "" {
		return nil
	}

	loc := parser.Location{File: filePath, Line: line}

	if !assigned {
		return []parser.Occurrence{{
			Name:     key,
			Kind:     parser.KindReference,
			Location: loc,
			Service:  service,
		}}
	}

	refs := interpolate.Find(value)
	if len(refs) == 0 {
		return []parser.Occurrence{{
			Name:     key,
			Kind:     parser.KindDefinition,
			Value:    value,
			Location: loc,
			Service:  service,
		}}
	}

	var out []parser.Occurrence
	keyCovered := false

	for _, ref := range refs {
		out = append(out, parser.Occurrence{
			Name:       ref.Name,
			Kind:       parser.KindReference,
			Location:   loc,
			Service:    service,
			HasDefault: ref.HasDefault,
		})
		if ref.Name == key {
			keyCovered = true
		}
	}

	unresolved := interpolate.Unresolved(refs)

	// "DB_HOST: ${POSTGRES_HOST}" renames a variable on the way in. The service does receive DB_HOST, but only if POSTGRES_HOST resolves.
	if !keyCovered {
		out = append(out, parser.Occurrence{
			Name:        key,
			Kind:        parser.KindReference,
			Location:    loc,
			Service:     service,
			HasDefault:  len(unresolved) == 0,
			DerivedFrom: unresolved,
		})
	}

	return out
}

func envFiles(service *yaml.Node, dir string) []string {
	node := mapValue(service, "env_file")
	if node == nil {
		return nil
	}

	var out []string
	add := func(p string) {
		if p != "" {
			out = append(out, path.Clean(path.Join(dir, p)))
		}
	}

	switch node.Kind {
	case yaml.ScalarNode:
		add(node.Value)
	case yaml.SequenceNode:
		for _, item := range node.Content {
			switch item.Kind {
			case yaml.ScalarNode:
				add(item.Value)
			case yaml.MappingNode:
				if p := mapValue(item, "path"); p != nil {
					add(p.Value)
				}
			}
		}
	}

	return out
}

// mapValue returns the value node for key in a mapping, or nil. Mapping content alternates key, value, key, value.
func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
