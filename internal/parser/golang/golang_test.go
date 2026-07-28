package golang_test

import (
	"testing"

	envparser "github.com/PeacexF/EnvGraph/internal/parser"
	"github.com/PeacexF/EnvGraph/internal/parser/golang"
)

// names returns the variable names a source file reads, in order.
func names(t *testing.T, src string) []string {
	t.Helper()

	res, err := golang.Parse("main.go", []byte(src))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	out := make([]string, 0, len(res.Occurrences))
	for _, occ := range res.Occurrences {
		if occ.Kind != envparser.KindConsumption {
			t.Errorf("occurrence %+v: kind = %q, want %q",
				occ, occ.Kind, envparser.KindConsumption)
		}
		out = append(out, occ.Name)
	}
	return out
}

func assertNames(t *testing.T, src string, want []string) {
	t.Helper()

	got := names(t, src)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
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
		{
			name: "getenv and lookupenv",
			src: `package main
import "os"
func main() {
	a := os.Getenv("A")
	b, _ := os.LookupEnv("B")
	_, _ = a, b
}`,
			want: []string{"A", "B"},
		},
		{
			name: "aliased import",
			src: `package main
import stdos "os"
func main() { _ = stdos.Getenv("ALIASED") }`,
			want: []string{"ALIASED"},
		},
		{
			name: "nested in another call",
			src: `package main
import (
	"fmt"
	"os"
)
func main() { fmt.Println(os.Getenv("NESTED")) }`,
			want: []string{"NESTED"},
		},
		{
			name: "inside a closure",
			src: `package main
import "os"
func main() {
	f := func() string { return os.Getenv("CLOSURE") }
	_ = f
}`,
			want: []string{"CLOSURE"},
		},
		{
			name: "in a struct literal",
			src: `package main
import "os"
type Config struct{ URL string }
func main() { _ = Config{URL: os.Getenv("IN_LITERAL")} }`,
			want: []string{"IN_LITERAL"},
		},
		{
			name: "at package level",
			src: `package main
import "os"
var Port = os.Getenv("PACKAGE_LEVEL")
func main() {}`,
			want: []string{"PACKAGE_LEVEL"},
		},
		{
			name: "in a method",
			src: `package main
import "os"
type T struct{}
func (T) Load() string { return os.Getenv("IN_METHOD") }`,
			want: []string{"IN_METHOD"},
		},
		{
			name: "several on one line",
			src: `package main
import "os"
func main() { _, _ = os.Getenv("X"), os.Getenv("Y") }`,
			want: []string{"X", "Y"},
		},
		{
			name: "the same variable twice",
			src: `package main
import "os"
func main() {
	_ = os.Getenv("SAME")
	_ = os.Getenv("SAME")
}`,
			want: []string{"SAME", "SAME"},
		},
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
		{
			name: "no os import",
			src: `package main
func main() { _ = Getenv("NOPE") }
func Getenv(string) string { return "" }`,
			want: nil,
		},
		{
			name: "blank import",
			src: `package main
import _ "os"
func main() {}`,
			want: nil,
		},
		{
			name: "dot import",
			src: `package main
import . "os"
func main() { _ = Getenv("DOTTED") }`,
			want: nil,
		},
		{
			name: "commented-out call",
			src: `package main
import "os"
// os.Getenv("COMMENTED")
func main() { _ = os.Getenv("REAL") }`,
			want: []string{"REAL"},
		},
		{
			name: "call named inside a string",
			src: `package main
import "os"
func main() {
	msg := ` + "`" + `use os.Getenv("QUOTED")` + "`" + `
	_ = msg
	_ = os.Getenv("REAL")
}`,
			want: []string{"REAL"},
		},
		{
			name: "computed name",
			src: `package main
import "os"
func main() {
	prefix := "APP_"
	_ = os.Getenv(prefix + "KEY")
	_ = os.Getenv("STATIC")
}`,
			want: []string{"STATIC"},
		},
		{
			name: "name held in a variable",
			src: `package main
import "os"
func main() {
	key := "DYNAMIC"
	_ = os.Getenv(key)
}`,
			want: nil,
		},
		{
			name: "another package's Getenv",
			src: `package main
import (
	"os"
	"example.com/conf"
)
func main() {
	_ = conf.Getenv("NOT_OS")
	_ = os.Getenv("YES")
}`,
			want: []string{"YES"},
		},
		{
			name: "a shadowing local named os",
			src: `package main
import "os"
type fake struct{}
func (fake) Getenv(string) string { return "" }
func main() {
	_ = os.Getenv("REAL")
}`,
			want: []string{"REAL"},
		},
		{
			name: "os.Environ takes no name",
			src: `package main
import "os"
func main() { _ = os.Environ() }`,
			want: nil,
		},
		{
			name: "os.Setenv is a write, not a read",
			src: `package main
import "os"
func main() { _ = os.Setenv("WRITTEN", "x") }`,
			want: nil,
		},
		{
			name: "empty name",
			src: `package main
import "os"
func main() { _ = os.Getenv("") }`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNames(t, tt.src, tt.want)
		})
	}
}

func TestLocations(t *testing.T) {
	res, err := golang.Parse("cmd/api/main.go", []byte(`package main

import "os"

func main() {
	_ = os.Getenv("PORT")
}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(res.Occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(res.Occurrences))
	}
	if got := res.Occurrences[0].Location.Line; got != 6 {
		t.Errorf("line = %d, want 6", got)
	}
	if got := res.Occurrences[0].Location.File; got != "cmd/api/main.go" {
		t.Errorf("file = %q, want cmd/api/main.go", got)
	}
}

func TestNoServicesReported(t *testing.T) {
	res, err := golang.Parse("main.go", []byte("package main\nimport \"os\"\nvar X = os.Getenv(\"A\")\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(res.Services) != 0 {
		t.Errorf("services = %+v, want none", res.Services)
	}
}

func TestSyntaxError(t *testing.T) {
	for _, src := range []string{
		"package main\nfunc main() {",
		"this is not go at all",
		"",
	} {
		if _, err := golang.Parse("bad.go", []byte(src)); err == nil {
			t.Errorf("Parse(%q) error = nil, want a parse error", src)
		}
	}
}
