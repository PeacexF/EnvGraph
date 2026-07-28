// Package graph holds the node/edge model that EnvGraph exports and renders
package graph

import (
	"encoding/json"
	"sort"
	"strconv"
)

// NodeType classifies a node in the configuration graph.
type NodeType string

const (
	NodeFile     NodeType = "file"
	NodeService  NodeType = "service"
	NodeVariable NodeType = "variable"
)

// Relationship names an edge.
type Relationship string

const (
	RelDefines    Relationship = "defines"
	RelDeclares   Relationship = "declares"
	RelPassedTo   Relationship = "passed_to"
	RelConsumedBy Relationship = "consumed_by"
)

// Node is one vertex of the graph.
type Node struct {
	ID   string   `json:"id"`
	Type NodeType `json:"type"`
	Name string   `json:"name"`

	// File and Line are empty on variables, which have no single location.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`

	// Category refines a file node: "env", "compose", "go", or "python".
	Category string `json:"category,omitempty"`

	// Status is set on variables: "ok", "missing", or "unused".
	Status string `json:"status,omitempty"`
}

// Edge points the way configuration flows. File and Line record where the relationship is written down, so the viewer can link back to it.
type Edge struct {
	From         string       `json:"from"`
	To           string       `json:"to"`
	Relationship Relationship `json:"relationship"`
	File         string       `json:"file,omitempty"`
	Line         int          `json:"line,omitempty"`
}

// Graph is a set of nodes and edges, each unique.
type Graph struct {
	nodes map[string]Node
	edges map[string]Edge
}

func New() *Graph {
	return &Graph{
		nodes: make(map[string]Node),
		edges: make(map[string]Edge),
	}
}

func FileID(path string) string { return "file:" + path }

// ServiceID includes the file because two compose files may each declare "api".
func ServiceID(composeFile, name string) string {
	return "service:" + composeFile + ":" + name
}

func VariableID(name string) string { return "var:" + name }

// AddNode keeps the first insertion of an ID, except that a later non-empty status fills an empty one.
func (g *Graph) AddNode(n Node) {
	existing, ok := g.nodes[n.ID]
	if !ok {
		g.nodes[n.ID] = n
		return
	}
	if existing.Status == "" && n.Status != "" {
		existing.Status = n.Status
		g.nodes[n.ID] = existing
	}
}

// AddEdge keeps edges that differ only by location apart, so every place a relationship is written down stays visible.
func (g *Graph) AddEdge(e Edge) {
	key := e.From + "\x00" + e.To + "\x00" + string(e.Relationship) +
		"\x00" + e.File + "\x00" + strconv.Itoa(e.Line)
	if _, ok := g.edges[key]; !ok {
		g.edges[key] = e
	}
}

// Nodes returns the nodes sorted by ID.
func (g *Graph) Nodes() []Node {
	out := make([]Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Edges returns the edges in a stable order.
func (g *Graph) Edges() []Edge {
	out := make([]Edge, 0, len(g.edges))
	for _, e := range g.edges {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.From != b.From:
			return a.From < b.From
		case a.To != b.To:
			return a.To < b.To
		case a.Relationship != b.Relationship:
			return a.Relationship < b.Relationship
		case a.File != b.File:
			return a.File < b.File
		default:
			return a.Line < b.Line
		}
	})
	return out
}

type document struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// MarshalJSON emits a stable order, so an unchanged project re-scans to a byte-identical file.
func (g *Graph) MarshalJSON() ([]byte, error) {
	return json.Marshal(document{Nodes: g.Nodes(), Edges: g.Edges()})
}
