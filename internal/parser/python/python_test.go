package python_test

import (
	"testing"

	"github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/python"
)

func names(t *testing.T, src string) []string {
	t.Helper()

	res, err := python.Parse("app.py", []byte(src))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	out := make([]string, 0, len(res.Occurrences))
	for _, occ := range res.Occurrences {
		if occ.Kind != parser.KindConsumption {
			t.Errorf("occurrence %+v: kind = %q, want %q",
				occ, occ.Kind, parser.KindConsumption)
		}
		out = append(out, occ.Name)
	}
	return out
}

func assertNames(t *testing.T, src string, want []string) {
	t.Helper()

	got := names(t, src)
	if len(got) != len(want) {
		t.Fatalf("Parse(%q) = %v, want %v", src, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("name %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFindsEnvReads(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"os.getenv", `x = os.getenv("A")`, []string{"A"}},
		{"single quotes", `x = os.getenv('A')`, []string{"A"}},
		{"bare getenv", "from os import getenv\nx = getenv(\"B\")", []string{"B"}},
		{"environ.get", `x = os.environ.get("C")`, []string{"C"}},
		{"environ index", `x = os.environ["D"]`, []string{"D"}},
		{"environ index bare", `x = environ['E']`, []string{"E"}},
		{"setdefault", `os.environ.setdefault("F", "1")`, []string{"F"}},
		{"with a fallback", `x = os.getenv("G", "fallback")`, []string{"G"}},
		{"whitespace inside the call", `x = os.getenv( "H" )`, []string{"H"}},
		{"whitespace inside the index", `x = os.environ[ "I" ]`, []string{"I"}},
		{"two on one line", `a, b = os.getenv("J"), os.getenv("K")`, []string{"J", "K"}},
		{"inside a function", "def load():\n    return os.getenv(\"L\")", []string{"L"}},
		{"inside a dict", `cfg = {"host": os.getenv("M")}`, []string{"M"}},
		{"chained with a cast", `port = int(os.environ["N"])`, []string{"N"}},
		{"hash inside a name", `x = os.getenv("O#P")`, []string{"O#P"}},
		{"lowercase name", `x = os.getenv("lower_case")`, []string{"lower_case"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNames(t, tt.src, tt.want)
		})
	}
}

func TestIgnoresNonReads(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"whole-line comment", `# os.getenv("A")`, nil},
		{"indented comment", `    # os.getenv("A")`, nil},
		{"trailing comment", `x = os.getenv("B")  # os.getenv("C")`, []string{"B"}},
		{"unrelated call", `x = get("D")`, nil},
		{"unrelated attribute", `x = config.getenv("E")`, []string{"E"}},
		{"no quotes", `x = os.getenv(key)`, nil},
		{"empty name", `x = os.getenv("")`, nil},
		{"empty file", ``, nil},
		{"only comments", "# one\n# two", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNames(t, tt.src, tt.want)
		})
	}
}

func TestHashInsideStringsIsNotAComment(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"double quoted", `x = "a # b"; y = os.getenv("REAL")`, []string{"REAL"}},
		{"single quoted", `x = 'a # b'; y = os.getenv("REAL")`, []string{"REAL"}},
		{"escaped quote", `x = "esc \" # still"; y = os.getenv("REAL")`, []string{"REAL"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNames(t, tt.src, tt.want)
		})
	}
}

func TestOverlappingPatternsReportOnce(t *testing.T) {
	// "os.environ.get" matches the getenv pattern too.
	assertNames(t, `x = os.environ.get("ONCE")`, []string{"ONCE"})
}

func TestTheSameNameOnDifferentLinesIsReportedTwice(t *testing.T) {
	assertNames(t, "a = os.getenv(\"SAME\")\nb = os.getenv(\"SAME\")",
		[]string{"SAME", "SAME"})
}

func TestLocations(t *testing.T) {
	res, err := python.Parse("app/settings.py", []byte("import os\n\nPORT = os.getenv(\"PORT\")\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(res.Occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(res.Occurrences))
	}
	if got := res.Occurrences[0].Location.Line; got != 3 {
		t.Errorf("line = %d, want 3", got)
	}
	if got := res.Occurrences[0].Location.File; got != "app/settings.py" {
		t.Errorf("file = %q, want app/settings.py", got)
	}
}

func TestWindowsLineEndings(t *testing.T) {
	res, err := python.Parse("app.py", []byte("import os\r\nX = os.getenv(\"A\")\r\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(res.Occurrences) != 1 || res.Occurrences[0].Location.Line != 2 {
		t.Errorf("occurrences = %+v, want A on line 2", res.Occurrences)
	}
}

func TestNoServicesReported(t *testing.T) {
	res, err := python.Parse("app.py", []byte(`x = os.getenv("A")`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(res.Services) != 0 {
		t.Errorf("services = %+v, want none", res.Services)
	}
}
