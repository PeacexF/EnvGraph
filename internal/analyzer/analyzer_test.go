package analyzer_test

import (
	"testing"

	"github.com/PeacexF/EnvGraph/internal/analyzer"
	"github.com/PeacexF/EnvGraph/internal/graph"
	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/scanner"
)

func at(file string, line int) parser.Location {
	return parser.Location{File: file, Line: line}
}

func def(name, file string, line int) parser.Occurrence {
	return parser.Occurrence{Name: name, Kind: parser.KindDefinition, Location: at(file, line)}
}

func use(name, file string, line int) parser.Occurrence {
	return parser.Occurrence{Name: name, Kind: parser.KindConsumption, Location: at(file, line)}
}

func ref(name, service string, line int) parser.Occurrence {
	return parser.Occurrence{
		Name:     name,
		Kind:     parser.KindReference,
		Location: at("docker-compose.yml", line),
		Service:  service,
	}
}

func analyze(occurrences []parser.Occurrence, services ...parser.Service) *analyzer.Report {
	return analyzer.Analyze(&scanner.Result{Occurrences: occurrences, Services: services})
}

// variable returns the analyzed variable by name, or fails the test.
func variable(t *testing.T, r *analyzer.Report, name string) analyzer.Variable {
	t.Helper()

	for _, v := range r.Variables {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("no variable %q in %+v", name, r.Variables)
	return analyzer.Variable{}
}

func assertStatus(t *testing.T, r *analyzer.Report, name string, want analyzer.Status) {
	t.Helper()

	if got := variable(t, r, name).Status; got != want {
		t.Errorf("%s status = %q, want %q", name, got, want)
	}
}

func TestStatuses(t *testing.T) {
	report := analyze([]parser.Occurrence{
		def("OK", ".env", 1),
		use("OK", "main.go", 5),

		use("MISSING", "main.go", 6),

		def("UNUSED", ".env", 2),

		// Injected into a container counts as used: the code reading it may
		// live inside the image.
		def("INJECTED", ".env", 3),
		ref("INJECTED", "api", 5),
	})

	assertStatus(t, report, "OK", analyzer.StatusOK)
	assertStatus(t, report, "MISSING", analyzer.StatusMissing)
	assertStatus(t, report, "UNUSED", analyzer.StatusUnused)
	assertStatus(t, report, "INJECTED", analyzer.StatusOK)
}

func TestMissingAndUnusedFilters(t *testing.T) {
	report := analyze([]parser.Occurrence{
		def("OK", ".env", 1),
		use("OK", "main.go", 1),
		use("GONE", "main.go", 2),
		def("SPARE", ".env", 2),
	})

	if missing := report.Missing(); len(missing) != 1 || missing[0].Name != "GONE" {
		t.Errorf("Missing() = %+v, want GONE", missing)
	}
	if unused := report.Unused(); len(unused) != 1 || unused[0].Name != "SPARE" {
		t.Errorf("Unused() = %+v, want SPARE", unused)
	}
}

func TestComposeReferenceIsNotASource(t *testing.T) {
	report := analyze([]parser.Occurrence{
		ref("DATABASE_URL", "api", 5),
		use("DATABASE_URL", "main.go", 9),
	})

	v := variable(t, report, "DATABASE_URL")
	if v.Status != analyzer.StatusMissing {
		t.Errorf("status = %q, want %q", v.Status, analyzer.StatusMissing)
	}
	if len(v.Sources) != 0 {
		t.Errorf("sources = %+v, want none: ${VAR} passes a value along", v.Sources)
	}
}

func TestDefaultCountsAsASource(t *testing.T) {
	withDefault := ref("PORT", "api", 7)
	withDefault.HasDefault = true

	report := analyze([]parser.Occurrence{withDefault, use("PORT", "main.go", 3)})

	v := variable(t, report, "PORT")
	if v.Status != analyzer.StatusOK {
		t.Errorf("status = %q, want %q", v.Status, analyzer.StatusOK)
	}
	if len(v.Sources) != 1 || !v.Sources[0].FromDefault {
		t.Errorf("sources = %+v, want one marked as a default", v.Sources)
	}
}

func TestValuesAreKept(t *testing.T) {
	occ := def("TOKEN", ".env", 1)
	occ.Value = "s3cret"

	v := variable(t, analyze([]parser.Occurrence{occ}), "TOKEN")
	if len(v.Sources) != 1 || v.Sources[0].Value != "s3cret" {
		t.Errorf("sources = %+v, want the assigned value preserved", v.Sources)
	}
}

func derived(name string, from []string, line int) parser.Occurrence {
	occ := ref(name, "web", line)
	occ.DerivedFrom = from
	return occ
}

func TestDerivedResolvesThroughItsInput(t *testing.T) {
	report := analyze([]parser.Occurrence{
		def("POSTGRES_HOST", ".env", 1),
		ref("POSTGRES_HOST", "web", 5),
		derived("DB_HOST", []string{"POSTGRES_HOST"}, 5),
		use("DB_HOST", "app.py", 3),
	})

	v := variable(t, report, "DB_HOST")
	if v.Status != analyzer.StatusOK {
		t.Errorf("status = %q, want %q", v.Status, analyzer.StatusOK)
	}
	if len(v.Sources) != 1 || len(v.Sources[0].DerivedFrom) != 1 {
		t.Errorf("sources = %+v, want one derived source", v.Sources)
	}
}

func TestDerivedStaysMissingWhenItsInputIs(t *testing.T) {
	report := analyze([]parser.Occurrence{
		derived("DB_HOST", []string{"POSTGRES_HOST"}, 5),
		use("DB_HOST", "app.py", 3),
	})

	assertStatus(t, report, "DB_HOST", analyzer.StatusMissing)
}

func TestDerivedNeedsEveryInput(t *testing.T) {
	report := analyze([]parser.Occurrence{
		def("USER", ".env", 1),
		derived("DSN", []string{"USER", "PASSWORD"}, 5),
		use("DSN", "app.py", 3),
	})

	assertStatus(t, report, "DSN", analyzer.StatusMissing)
}

func TestDerivedChainResolves(t *testing.T) {
	report := analyze([]parser.Occurrence{
		derived("A", []string{"B"}, 3),
		derived("B", []string{"C"}, 4),
		derived("C", []string{"D"}, 5),
		def("D", ".env", 1),
	})

	for _, name := range []string{"A", "B", "C", "D"} {
		if v := variable(t, report, name); len(v.Sources) == 0 {
			t.Errorf("%s has no source, want the chain to resolve", name)
		}
	}
}

func TestDerivedCycleDoesNotHang(t *testing.T) {
	report := analyze([]parser.Occurrence{
		derived("A", []string{"B"}, 3),
		derived("B", []string{"A"}, 4),
	})

	for _, name := range []string{"A", "B"} {
		assertStatus(t, report, name, analyzer.StatusMissing)
	}
}

func TestEnvFileReachesService(t *testing.T) {
	report := analyze(
		[]parser.Occurrence{
			def("SECRET", ".env", 1),
			def("ELSEWHERE", "other/.env", 1),
		},
		parser.Service{
			Name:     "web",
			Location: at("docker-compose.yml", 2),
			EnvFiles: []string{".env"},
		},
	)

	v := variable(t, report, "SECRET")
	if len(v.PassedTo) != 1 || v.PassedTo[0].Service != "web" {
		t.Errorf("SECRET passedTo = %+v, want web", v.PassedTo)
	}
	assertStatus(t, report, "SECRET", analyzer.StatusOK)

	if other := variable(t, report, "ELSEWHERE"); len(other.PassedTo) != 0 {
		t.Errorf("ELSEWHERE passedTo = %+v, want none: that file is not loaded", other.PassedTo)
	}
}

func TestEnvFileDoesNotInjectReferences(t *testing.T) {
	// Only definitions live in an env file; a stray reference must not be
	// treated as something the service receives.
	report := analyze(
		[]parser.Occurrence{{
			Name:     "PASSTHROUGH",
			Kind:     parser.KindReference,
			Location: at(".env", 1),
		}},
		parser.Service{
			Name:     "web",
			Location: at("docker-compose.yml", 2),
			EnvFiles: []string{".env"},
		},
	)

	if v := variable(t, report, "PASSTHROUGH"); len(v.PassedTo) != 0 {
		t.Errorf("passedTo = %+v, want none", v.PassedTo)
	}
}

func TestDuplicatesAreCollapsed(t *testing.T) {
	dup := use("A", "main.go", 5)
	report := analyze([]parser.Occurrence{dup, dup, dup})

	if v := variable(t, report, "A"); len(v.Consumers) != 1 {
		t.Errorf("consumers = %+v, want one", v.Consumers)
	}
}

func TestDistinctLocationsAreKept(t *testing.T) {
	report := analyze([]parser.Occurrence{
		use("A", "main.go", 5),
		use("A", "main.go", 9),
		use("A", "other.go", 5),
	})

	if v := variable(t, report, "A"); len(v.Consumers) != 3 {
		t.Errorf("consumers = %+v, want all three", v.Consumers)
	}
}

func TestVariablesAreSorted(t *testing.T) {
	report := analyze([]parser.Occurrence{
		def("ZEBRA", ".env", 1),
		def("ALPHA", ".env", 2),
		def("MIKE", ".env", 3),
	})

	for i, want := range []string{"ALPHA", "MIKE", "ZEBRA"} {
		if report.Variables[i].Name != want {
			t.Errorf("variable %d = %q, want %q", i, report.Variables[i].Name, want)
		}
	}
}

func TestEmptyScan(t *testing.T) {
	report := analyze(nil)

	if len(report.Variables) != 0 {
		t.Errorf("variables = %+v, want none", report.Variables)
	}
	if len(report.Missing()) != 0 || len(report.Unused()) != 0 {
		t.Error("an empty scan should report no findings")
	}
}

func TestGraphCoversTheWholeFlow(t *testing.T) {
	res := &scanner.Result{
		Files: []scanner.File{
			{Path: ".env", Type: scanner.TypeEnv},
			{Path: "docker-compose.yml", Type: scanner.TypeCompose},
			{Path: "main.go", Type: scanner.TypeGo},
		},
		Occurrences: []parser.Occurrence{
			def("DATABASE_URL", ".env", 1),
			ref("DATABASE_URL", "api", 5),
			use("DATABASE_URL", "main.go", 9),
		},
		Services: []parser.Service{
			{Name: "api", Location: at("docker-compose.yml", 2)},
		},
	}

	g := analyzer.Graph(res, analyzer.Analyze(res))

	nodes := make(map[string]graph.Node)
	for _, n := range g.Nodes() {
		nodes[n.ID] = n
	}

	varID := graph.VariableID("DATABASE_URL")
	if nodes[varID].Type != graph.NodeVariable {
		t.Errorf("variable node missing from %+v", nodes)
	}
	if nodes[varID].Status != string(analyzer.StatusOK) {
		t.Errorf("variable status = %q, want ok", nodes[varID].Status)
	}
	if got := nodes[graph.FileID(".env")].Category; got != string(scanner.TypeEnv) {
		t.Errorf(".env category = %q, want env", got)
	}

	svcID := graph.ServiceID("docker-compose.yml", "api")
	for _, want := range []struct {
		from, to string
		rel      graph.Relationship
	}{
		{graph.FileID(".env"), varID, graph.RelDefines},
		{varID, svcID, graph.RelPassedTo},
		{varID, graph.FileID("main.go"), graph.RelConsumedBy},
		{graph.FileID("docker-compose.yml"), svcID, graph.RelDeclares},
	} {
		found := false
		for _, e := range g.Edges() {
			if e.From == want.from && e.To == want.to && e.Relationship == want.rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing edge %s -%s-> %s", want.from, want.rel, want.to)
		}
	}
}

func TestGraphOmitsFilesThatTouchNoConfiguration(t *testing.T) {
	res := &scanner.Result{
		Files: []scanner.File{
			{Path: ".env", Type: scanner.TypeEnv},
			{Path: "unrelated.go", Type: scanner.TypeGo},
		},
		Occurrences: []parser.Occurrence{def("A", ".env", 1)},
	}

	g := analyzer.Graph(res, analyzer.Analyze(res))

	for _, n := range g.Nodes() {
		if n.ID == graph.FileID("unrelated.go") {
			t.Error("a file with no variables should stay out of the graph")
		}
	}
}

func TestGraphResolvesServicesDeclaredInAnotherFile(t *testing.T) {
	// A compose override adds variables to a service declared elsewhere.
	res := &scanner.Result{
		Occurrences: []parser.Occurrence{{
			Name:     "EXTRA",
			Kind:     parser.KindDefinition,
			Location: at("docker-compose.override.yml", 4),
			Service:  "api",
		}},
		Services: []parser.Service{
			{Name: "api", Location: at("docker-compose.yml", 2)},
		},
	}

	g := analyzer.Graph(res, analyzer.Analyze(res))

	found := false
	for _, e := range g.Edges() {
		if e.Relationship == graph.RelPassedTo &&
			e.To == graph.ServiceID("docker-compose.yml", "api") {
			found = true
		}
	}
	if !found {
		t.Errorf("edges = %+v, want EXTRA to reach the api service", g.Edges())
	}
}

func TestGraphSkipsAmbiguousServiceNames(t *testing.T) {
	// Two files declare "api", so an injection written in a third file
	// cannot be attributed to either.
	res := &scanner.Result{
		Occurrences: []parser.Occurrence{{
			Name:     "EXTRA",
			Kind:     parser.KindDefinition,
			Location: at("docker-compose.override.yml", 4),
			Service:  "api",
		}},
		Services: []parser.Service{
			{Name: "api", Location: at("a/docker-compose.yml", 2)},
			{Name: "api", Location: at("b/docker-compose.yml", 2)},
		},
	}

	g := analyzer.Graph(res, analyzer.Analyze(res))

	for _, e := range g.Edges() {
		if e.Relationship == graph.RelPassedTo {
			t.Errorf("edge %+v guesses at an ambiguous service name", e)
		}
	}
}

func TestGraphIsDeterministic(t *testing.T) {
	res := &scanner.Result{
		Files: []scanner.File{{Path: ".env", Type: scanner.TypeEnv}},
		Occurrences: []parser.Occurrence{
			def("B", ".env", 2),
			def("A", ".env", 1),
			use("C", "main.go", 1),
		},
	}

	first := analyzer.Graph(res, analyzer.Analyze(res)).Nodes()
	for i := range 20 {
		got := analyzer.Graph(res, analyzer.Analyze(res)).Nodes()
		if len(got) != len(first) {
			t.Fatalf("run %d produced %d nodes, want %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j].ID != first[j].ID {
				t.Fatalf("run %d differs at node %d: %q vs %q", i, j, got[j].ID, first[j].ID)
			}
		}
	}
}
