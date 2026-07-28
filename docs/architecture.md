# Architecture

How EnvGraph is put together, and how to extend it.

- [The shape of it](#the-shape-of-it)
- [Packages](#packages)
- [The data model](#the-data-model)
- [Stage by stage](#stage-by-stage)
- [Adding a source](#adding-a-source)
- [Design decisions](#design-decisions)
- [Testing](#testing)
- [The web viewer](#the-web-viewer)

---

## The shape of it

Four stages, each one depending only on the one before it:

```
   files on disk
        │
        ▼
   scanner ──────── walks the tree, picks a parser per file
        │
        ▼
   parsers ──────── one file → occurrences ("this file mentions FOO")
        │
        ▼
   analyzer ─────── occurrences → verdicts + graph
        │
        ▼
   cli / server ─── text, JSON, or an interactive page
```

The important property is that **parsers do not decide anything**. A parser
reports that a file mentions a variable and in what way; whether that makes the
variable missing, unused or fine is the analyzer's call. This is why adding a
new file format does not require touching any of the resolution logic.

---

## Packages

| Package | Lines | Does |
| --- | --- | --- |
| `internal/parser` | 56 | The vocabulary every parser speaks. No logic. |
| `internal/parser/env` | 152 | `.env` files |
| `internal/parser/compose` | 191 | Docker Compose |
| `internal/parser/dockerfile` | 240 | Dockerfiles |
| `internal/parser/workflow` | 286 | GitHub Actions |
| `internal/parser/golang` | 97 | Go source, via `go/ast` |
| `internal/parser/python` | 78 | Python source, via regex |
| `internal/parser/javascript` | 184 | JS/TS source, via regex over sanitised text |
| `internal/parser/interpolate` | 108 | `$VAR` / `${VAR:-default}` syntax, shared by compose and Dockerfile |
| `internal/scanner` | 262 | Walks the tree, classifies files, dispatches to parsers |
| `internal/analyzer` | 425 | Verdicts, derived-variable resolution, graph construction |
| `internal/graph` | 141 | Node/edge model with stable ordering |
| `internal/config` | 145 | `.envgraph.yml` and ignore rules |
| `internal/cli` | 783 | Commands, flags, rendering |
| `internal/server` | 90 | HTTP API and static assets for the viewer |
| `web` | — | The viewer: HTML, CSS, ES modules, vendored Cytoscape |

Dependencies point one way. `parser` imports nothing at all — not even from the
standard library. `scanner` imports the parsers; `analyzer` imports `scanner` and
`graph`; `cli` and `server` sit on top, and only `cmd/envgraph` imports `cli`.

Third-party dependencies are two: `spf13/cobra` for the CLI and `gopkg.in/yaml.v3`
for YAML. That is deliberate — see [Design decisions](#design-decisions).

---

## The data model

### Occurrence

What a parser produces. One mention of one variable in one file.

```go
type Occurrence struct {
    Name     string
    Kind     Kind      // definition | reference | consumption
    Value    string    // for definitions
    Location Location  // slash-separated path relative to the scan root, plus line

    Service     string   // set when it belongs to a container or job
    HasDefault  bool     // ${VAR:-fallback} — can never resolve to nothing
    DerivedFrom []string // built from these variables
    Origin      string   // supplied externally, e.g. a GitHub secret
}
```

The three kinds:

| Kind | Means | Example |
| --- | --- | --- |
| `definition` | This file supplies a value | `DATABASE_URL=postgres://…` |
| `reference` | This file reads a value from elsewhere | `${DATABASE_URL}` in compose |
| `consumption` | Application code reads it at runtime | `os.Getenv("DATABASE_URL")` |

The distinction between `reference` and `consumption` is what makes the strict
source rule possible. A compose file passing `${FOO}` through is not the same as
supplying `FOO`, and the model has to be able to say so.

### Service

A named runtime context that variables flow into. Compose services and GitHub
Actions jobs are both modelled as `Service` — they behave identically as far as
the graph is concerned, so workflow support needed no new node type.

### Variable

What the analyzer produces: an `Occurrence` set folded into one verdict.

```go
type Variable struct {
    Name      string
    Status    Status      // ok | missing | unused
    Sources   []Source    // where a value comes from
    PassedTo  []Injection // containers and jobs that receive it
    Consumers []Location  // where it is read
}
```

### Graph

`file`, `service` and `variable` nodes joined by `defines`, `declares`,
`passed_to` and `consumed_by` edges. IDs are prefixed (`file:`, `var:`,
`service:<file>:<name>`) so the three kinds can never collide, and a service ID
includes its file because two compose files may each declare `api`.

Nodes and edges are emitted in sorted order, so an unchanged project exports
byte-identically and the output diffs cleanly.

---

## Stage by stage

### Scanning

`scanner.Scan` walks from the root with `filepath.WalkDir`, skipping dependency
and build directories, then classifies each file by path and hands it to a
parser.

Paths are made relative to the scan root and converted to forward slashes
immediately, so everything downstream — locations, graph IDs, `env_file`
resolution — works identically on Windows.

Failures are collected, not raised. A malformed compose file becomes an entry in
`Result.Errors` and a warning on stderr; the rest of the scan proceeds.

### Analysis

`analyzer.Analyze` folds occurrences into variables:

1. Definitions become sources. References with a fallback become sources too.
   References that are *derived* from other variables are set aside.
2. Consumptions become consumers. So do references outside a container — a
   Dockerfile's `ENV PATH=$APP_HOME/bin` really does read `APP_HOME`.
3. Occurrences carrying a `Service` are recorded as reaching that container or
   job.
4. Services that load an `env_file` receive every variable defined in it.
5. **Derived variables resolve to a fixed point.** A variable built from others
   is supplied exactly when all of its inputs are. One pass is not enough,
   because a derived variable can feed another, so the loop repeats until
   nothing new resolves. A cycle stops making progress and exits rather than
   spinning.
6. Verdicts are assigned, duplicates collapsed, variables sorted.

`analyzer.Graph` then renders the report as nodes and edges. Files are added on
demand, so a source file that never touches configuration stays out.

### Filtering

`Report.Without` drops ignored variables. Because `Graph` builds from the
report, filtering there removes them from the graph too — every command sees the
same set.

---

## Adding a source

Say you want Kubernetes manifests. The whole change is five steps.

**1. Write the parser.** One exported function, in its own package:

```go
package kubernetes

func Parse(filePath string, content []byte) (parser.Result, error)
```

`filePath` is already relative to the scan root and slash-separated. Return
occurrences; do not decide whether anything is missing. If the file is malformed,
return an error — the scanner will report it and carry on.

If the format uses shell-style `${VAR}` syntax, use `internal/parser/interpolate`
rather than writing it again.

**2. Add a file type** in `internal/scanner/scanner.go`:

```go
const TypeKubernetes FileType = "kubernetes"
```

**3. Classify it**, in `classify`. It receives the path relative to the root, so
directory-based rules are possible — that is how workflows are recognised:

```go
case isKubernetes(rel):
    return TypeKubernetes, true
```

Order matters where patterns overlap. Workflows are checked before compose so a
workflow named `docker-compose.yml` is still read as a workflow.

**4. Dispatch it**, in `parse`:

```go
case TypeKubernetes:
    return kubernetes.Parse(rel, content)
```

**5. Test it black-box**, from `package kubernetes_test`, through `Parse` only.

That is all. No changes to the analyzer, the graph, the CLI or the viewer —
they work in terms of occurrences, and you just produced some.

### Getting the semantics right

The judgement calls that matter, using compose as the worked example:

- `DATABASE_URL: ${DATABASE_URL}` → `KindReference`. It passes a value along; it
  does not supply one.
- `PORT: ${PORT:-8080}` → `KindReference` with `HasDefault`. The fallback means
  it can never be empty, so it counts as a source.
- `DB_HOST: ${POSTGRES_HOST}` → a reference to `POSTGRES_HOST`, plus a reference
  for `DB_HOST` with `DerivedFrom: ["POSTGRES_HOST"]`. The service really does
  receive `DB_HOST`, but only if the other resolves.
- `LOG_LEVEL: info` → `KindDefinition`. A literal is a source.

When in doubt: **would this file alone guarantee the process sees a value?** If
yes it is a definition; if it depends on something else, it is a reference.

---

## Design decisions

**Strict about what supplies a value.** A checker that treats every mention as a
source never reports the bug you have. `${VAR}` in compose being a pass-through
rather than a source is the single most consequential decision in the codebase,
and derived variables exist so that strictness does not produce false positives
on renames.

**Two dependencies.** Cobra and yaml.v3. `.env` and Dockerfile parsing are
hand-written, Go source uses `go/ast` from the standard library, and the viewer
ships one vendored file. Every parser stays debuggable with a stack trace, and
`go build` is the only build step.

**`go/ast` for Go, regex for the rest.** Go gets a real parser because the
standard library provides one: it ignores commented-out code, sees through
aliased imports, and gives exact positions. Python and JavaScript have no such
option in Go, so they match text — but JavaScript sanitises comments and strings
first, tracking template-literal interpolation, because `${...}` inside a
backtick string is executable code.

**Values are redacted by default everywhere.** `.env` files hold credentials, so
`scan -f json`, `export` and the HTTP API all strip values unless asked. `serve`
binds to loopback for the same reason.

**Parse failures are warnings.** One bad file must not hide a whole project.

**Stable output.** Sorted nodes and edges mean an unchanged project produces an
identical file, so a committed `graph.json` diffs meaningfully.

---

## Testing

Tests are **black-box**: every test file is `package foo_test` and exercises only
the exported API. If something is hard to test that way, that is usually a signal
about the API rather than about the test.

| Layer | Where |
| --- | --- |
| Per-package unit tests | Beside each package |
| End-to-end over the examples | `tests/` |
| Fixture projects | `examples/` |

The examples carry deliberately broken configuration — a missing variable, an
unused one — so CI can assert that `check` *fails* on them. A passing run there
means detection has regressed.

Run what CI runs:

```bash
gofmt -l ./cmd ./internal ./tests ./web   # must print nothing
go vet ./...
go test -race ./...
```

CI also runs the built binary against the examples, boots `serve` and pulls every
asset, and runs `envgraph check . --strict` on this repository — the viewer only
fails at runtime, where the embedded assets and the API meet.

---

## The web viewer

`envgraph serve` starts an HTTP server with two endpoints and a static page:

| Route | Serves |
| --- | --- |
| `GET /api/graph` | The analysis and the graph, same shape as `scan -f json` |
| `GET /api/meta` | The scanned root and whether values are exposed |
| `GET /` | The page and its assets |

The project is re-scanned on **every** API request. There is no cache and no file
watcher: a scan is fast enough that reloading the page is the whole
freshness story.

Assets live in `web/` and are compiled in with `go:embed`, so the binary stays
self-contained and `go build` remains the only build step. There is no bundler
and no `npm install` — the page is plain ES modules plus one vendored copy of
Cytoscape (MIT, version recorded in `web/vendor/README.md`).

`web/force.js` is a continuous force simulation — pairwise repulsion, springs
along edges, gravity toward the centre of mass, velocity damping. Cytoscape's
built-in layouts run once and stop; this keeps running so the graph reacts when
dragged, and idles once kinetic energy falls below a threshold rather than
burning CPU on a settled graph. The three constants at the top of that file are
the tuning dials.

Colours come from a validated palette. Status is never carried by hue alone:
`ok`-green and `missing`-red are nearly indistinguishable under deuteranopia, so
each status also carries a glyph (`!`, `?`) and a border treatment.
