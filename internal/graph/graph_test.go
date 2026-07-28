package graph_test

import (
	"encoding/json"
	"testing"

	"github.com/PeacexF/EnvGraph/internal/graph"
)

func node(id string) graph.Node {
	return graph.Node{ID: id, Type: graph.NodeVariable, Name: id}
}

func TestIDsAreDistinct(t *testing.T) {
	ids := []string{
		graph.FileID("api"),
		graph.VariableID("api"),
		graph.ServiceID("docker-compose.yml", "api"),
	}

	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("id %q collides with another kind of node", id)
		}
		seen[id] = true
	}
}

func TestServiceIDsAreScopedToTheirFile(t *testing.T) {
	// Two compose files may each declare a service called "api".
	a := graph.ServiceID("docker-compose.yml", "api")
	b := graph.ServiceID("deploy/docker-compose.yml", "api")

	if a == b {
		t.Errorf("both service ids are %q, want them kept apart", a)
	}
}

func TestAddNodeIsIdempotent(t *testing.T) {
	g := graph.New()
	g.AddNode(node("var:A"))
	g.AddNode(node("var:A"))

	if got := len(g.Nodes()); got != 1 {
		t.Errorf("got %d nodes, want 1", got)
	}
}

func TestAddNodeFillsAnEmptyStatus(t *testing.T) {
	g := graph.New()
	g.AddNode(node("var:A"))

	withStatus := node("var:A")
	withStatus.Status = "missing"
	g.AddNode(withStatus)

	if got := g.Nodes()[0].Status; got != "missing" {
		t.Errorf("status = %q, want missing", got)
	}
}

func TestAddNodeKeepsAnExistingStatus(t *testing.T) {
	g := graph.New()

	first := node("var:A")
	first.Status = "ok"
	g.AddNode(first)

	second := node("var:A")
	second.Status = "missing"
	g.AddNode(second)

	if got := g.Nodes()[0].Status; got != "ok" {
		t.Errorf("status = %q, want the first insertion to win", got)
	}
}

func TestAddEdgeDeduplicates(t *testing.T) {
	g := graph.New()
	e := graph.Edge{From: "a", To: "b", Relationship: graph.RelDefines, File: ".env", Line: 1}

	g.AddEdge(e)
	g.AddEdge(e)

	if got := len(g.Edges()); got != 1 {
		t.Errorf("got %d edges, want 1", got)
	}
}

func TestAddEdgeKeepsSeparateLocations(t *testing.T) {
	g := graph.New()
	g.AddEdge(graph.Edge{From: "a", To: "b", Relationship: graph.RelDefines, File: ".env", Line: 1})
	g.AddEdge(graph.Edge{From: "a", To: "b", Relationship: graph.RelDefines, File: ".env", Line: 9})

	if got := len(g.Edges()); got != 2 {
		t.Errorf("got %d edges, want both lines kept", got)
	}
}

func TestAddEdgeKeepsSeparateRelationships(t *testing.T) {
	g := graph.New()
	g.AddEdge(graph.Edge{From: "a", To: "b", Relationship: graph.RelDefines})
	g.AddEdge(graph.Edge{From: "a", To: "b", Relationship: graph.RelPassedTo})

	if got := len(g.Edges()); got != 2 {
		t.Errorf("got %d edges, want both relationships kept", got)
	}
}

func TestEmptyGraph(t *testing.T) {
	g := graph.New()

	if len(g.Nodes()) != 0 || len(g.Edges()) != 0 {
		t.Errorf("new graph = %+v, want empty", g)
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(data) != `{"nodes":[],"edges":[]}` {
		t.Errorf("marshalled empty graph = %s, want empty arrays rather than null", data)
	}
}

func TestNodesAreSortedByID(t *testing.T) {
	g := graph.New()
	for _, id := range []string{"var:Z", "var:A", "var:M"} {
		g.AddNode(node(id))
	}

	want := []string{"var:A", "var:M", "var:Z"}
	for i, n := range g.Nodes() {
		if n.ID != want[i] {
			t.Errorf("node %d = %q, want %q", i, n.ID, want[i])
		}
	}
}

func TestEdgesAreOrderedStably(t *testing.T) {
	edges := []graph.Edge{
		{From: "b", To: "a", Relationship: graph.RelDefines},
		{From: "a", To: "z", Relationship: graph.RelDefines},
		{From: "a", To: "b", Relationship: graph.RelPassedTo, Line: 2},
		{From: "a", To: "b", Relationship: graph.RelPassedTo, Line: 1},
		{From: "a", To: "b", Relationship: graph.RelDefines},
	}

	// Insertion order must not affect the result.
	first := edgeKeys(build(edges))
	reversed := make([]graph.Edge, len(edges))
	for i, e := range edges {
		reversed[len(edges)-1-i] = e
	}
	second := edgeKeys(build(reversed))

	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("edge %d = %q from one order, %q from the other", i, first[i], second[i])
		}
	}
}

func build(edges []graph.Edge) *graph.Graph {
	g := graph.New()
	for _, e := range edges {
		g.AddEdge(e)
	}
	return g
}

func edgeKeys(g *graph.Graph) []string {
	out := make([]string, 0, len(g.Edges()))
	for _, e := range g.Edges() {
		out = append(out, e.From+"->"+e.To+":"+string(e.Relationship))
	}
	return out
}

func TestMarshalJSONShape(t *testing.T) {
	g := graph.New()
	g.AddNode(graph.Node{
		ID: "file:.env", Type: graph.NodeFile, Name: ".env",
		File: ".env", Category: "env",
	})
	g.AddNode(graph.Node{
		ID: "var:A", Type: graph.NodeVariable, Name: "A", Status: "ok",
	})
	g.AddEdge(graph.Edge{
		From: "file:.env", To: "var:A", Relationship: graph.RelDefines,
		File: ".env", Line: 1,
	})

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var doc struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(doc.Nodes) != 2 || len(doc.Edges) != 1 {
		t.Fatalf("got %d nodes and %d edges, want 2 and 1", len(doc.Nodes), len(doc.Edges))
	}

	// Nodes come back sorted by ID, so the file precedes the variable.
	file, variable := doc.Nodes[0], doc.Nodes[1]

	// Empty fields must be omitted, so the viewer sees no phantom values.
	if _, ok := file["status"]; ok {
		t.Errorf("file node = %v, want no status field", file)
	}
	if _, ok := file["category"]; !ok {
		t.Errorf("file node = %v, want a category field", file)
	}
	if _, ok := variable["category"]; ok {
		t.Errorf("variable node = %v, want no category field", variable)
	}
	if _, ok := variable["line"]; ok {
		t.Errorf("variable node = %v, want no line field", variable)
	}
}

func TestMarshalJSONIsStable(t *testing.T) {
	g := graph.New()
	for _, id := range []string{"var:C", "var:A", "var:B"} {
		g.AddNode(node(id))
		g.AddEdge(graph.Edge{From: "file:.env", To: id, Relationship: graph.RelDefines})
	}

	first, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	for i := range 20 {
		again, err := json.Marshal(g)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("run %d differs:\n%s\n%s", i, first, again)
		}
	}
}
