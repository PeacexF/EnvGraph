# Guide

Everything EnvGraph does, and why it decides what it decides.

- [Install](#install)
- [First run](#first-run)
- [Commands](#commands)
  - [scan](#scan)
  - [explain](#explain)
  - [check](#check)
  - [serve](#serve)
  - [export](#export)
- [What EnvGraph reads](#what-envgraph-reads)
- [How resolution works](#how-resolution-works)
- [Configuration](#configuration)
- [Using it in CI](#using-it-in-ci)
- [Secrets](#secrets)
- [When it gets something wrong](#when-it-gets-something-wrong)

---

## Install

```bash
go install github.com/PeacexF/EnvGraph/cmd/envgraph@latest
```

Or from a checkout:

```bash
go build -o envgraph ./cmd/envgraph
```

One binary, no runtime dependencies. The web viewer is compiled in.

---

## First run

```bash
envgraph scan .
```

```
Scanned 4 files, found 7 variables

DATABASE_URL  ok
  source     .env:2
  passed to  api (docker-compose.yml:5)
  used in    config/database.go:7

JWT_SECRET  missing
  source     (none)
  used in    cmd/api/main.go:14

OLD_API_KEY  unused
  source     .env:7
  used in    (nothing)

5 ok, 1 missing, 1 unused
```

Three verdicts, and they are the whole point of the tool:

| Status | Meaning |
| --- | --- |
| `ok` | Something supplies it and something uses it |
| `missing` | Something reads it, nothing supplies it |
| `unused` | Something supplies it, nothing reads it |

Every location is printed as `path:line`, which most terminals and editors turn
into a clickable link.

If the output is noisy on your project, that is expected on a first run — see
[Configuration](#configuration).

---

## Commands

All commands take an optional path, defaulting to the current directory, and
share the flags in [Configuration](#configuration).

### scan

Reports every variable.

```bash
envgraph scan .
envgraph scan . --only missing        # just the problems
envgraph scan . --only missing,unused
envgraph scan . -f json               # machine-readable
envgraph scan . -f json -o report.json
```

`--only` filters what is listed, not what was found: the tally at the bottom
still describes the whole project. In JSON it filters the `variables` array but
leaves the graph whole, because a graph filtered by status has edges pointing at
nodes that are no longer there.

### explain

Traces one variable. This is the fastest way to answer "where does this come
from", and it stays readable on a project where `scan` prints hundreds of lines.

```bash
envgraph explain DATABASE_URL
envgraph explain DATABASE_URL ./services/api
```

```
DATABASE_URL  ok

from   .env:2

into   api (docker-compose.yml:5)
       worker (docker-compose.yml:15)

read   config/database.go:7
```

Details worth knowing:

- A **missing** variable exits `0` and prints advice on how to supply it. You
  asked to see it; that is not a command failure.
- An **unknown name** exits `1` and suggests near misses, since a wrong-case
  name is the usual mistake:

  ```
  No variable named database_url in this project.

  Did you mean:
    DATABASE_URL
  ```

- It answers even for variables an [ignore rule](#configuration) hides from
  every other command, and tells you they are ignored. Asking by name is
  deliberate.

### check

The CI command. Prints findings and sets the exit code.

```bash
envgraph check .            # exit 1 when a variable is missing
envgraph check . --strict   # also exit 1 when a variable is unused
```

```
ERROR   JWT_SECRET is used but never provided
        used in cmd/api/main.go:14

WARNING OLD_API_KEY is defined but never used
        defined in .env:7

1 missing, 1 unused
```

| Exit code | Meaning |
| --- | --- |
| `0` | Nothing missing (and nothing unused, under `--strict`) |
| `1` | Findings, or the scan itself failed |

### serve

An interactive graph in the browser.

```bash
envgraph serve .
envgraph serve . --port 3000
```

Opens on `http://127.0.0.1:8080`. **Localhost only** by default — your
configuration is not something to publish on a shared network. `--host` overrides
that if you know you want it.

The layout is a live force simulation: drag a node and its neighbours follow, or
switch **Physics** off to freeze it. Click any node for the full trace, filter to
missing or unused, search by name.

The project is re-scanned on every request, so editing a `.env` and reloading the
page shows the new state without restarting.

### export

Writes the raw graph for other tools.

```bash
envgraph export .            # writes graph.json
envgraph export . -o -       # to stdout
```

```json
{
  "nodes": [
    { "id": "file:.env", "type": "file", "name": ".env", "category": "env" },
    { "id": "var:DATABASE_URL", "type": "variable", "name": "DATABASE_URL", "status": "ok" }
  ],
  "edges": [
    { "from": "file:.env", "to": "var:DATABASE_URL", "relationship": "defines", "file": ".env", "line": 2 }
  ]
}
```

Node types are `file`, `service`, `variable`. Relationships are `defines`,
`declares`, `passed_to`, `consumed_by`. Output is byte-stable: an unchanged
project re-exports identically, so it diffs cleanly in version control.

Use `scan -f json` instead when you want the per-variable analysis alongside the
graph.

---

## What EnvGraph reads

| Source | Detects |
| --- | --- |
| `.env`, `.env.*`, `*.env` | Assignments, `export` prefix, quoting, escapes, multi-line values, inline comments |
| `docker-compose.yml`, `compose.yaml` | `environment` (map and list forms), `env_file`, `${VAR}` substitution with defaults |
| `Dockerfile`, `Dockerfile.*`, `*.Dockerfile` | `ENV` (both forms), `ARG`, `$VAR` inside values, line continuations |
| `.github/workflows/*.yml` | `env:` at workflow, job and step level; `${{ secrets/vars/inputs/env }}`; shell variables in `run:` |
| `.go` | `os.Getenv`, `os.LookupEnv`, including aliased imports of `os` |
| `.py` | `os.getenv`, `os.environ[...]`, `os.environ.get`, `environ.setdefault` |
| `.js .jsx .mjs .cjs .ts .tsx .mts .cts` | `process.env.X`, `process.env["X"]`, destructuring with renames and defaults, `import.meta.env` |

Skipped by default: test files (`--include-tests` keeps them), TypeScript
declaration files, files over 2 MiB, and these directories:

```
.git  .hg  .svn  .idea  .vscode  node_modules  vendor  venv  .venv
__pycache__  dist  build  target  .next  .tox  .mypy_cache  .pytest_cache
```

A file that fails to parse is reported as a warning on stderr and the scan
continues. One malformed compose file will not hide the rest of your project.

---

## How resolution works

The rule for what counts as *supplying* a value is deliberately strict, because
a checker that accepts almost anything as a source never reports the bug you
actually have.

| Written as | Supplies a value? |
| --- | --- |
| `DATABASE_URL=postgres://…` in `.env` | Yes |
| `LOG_LEVEL: info` in compose | Yes |
| `PORT: ${PORT:-8080}` in compose | Yes — the fallback guarantees one |
| `ENV PORT=3000` in a Dockerfile | Yes |
| `ARG VERSION=1` in a Dockerfile | Yes, via the default |
| `KEY: ${{ secrets.KEY }}` in a workflow | Yes — GitHub supplies it |
| `KEY: ${{ vars.KEY }}` or `${{ inputs.x }}` | Yes, same reasoning |
| `DATABASE_URL: ${DATABASE_URL}` in compose | **No** — it passes a value along without supplying one |
| `- DATABASE_URL` in a compose list | **No** — it forwards a host variable |
| `ARG VERSION` in a Dockerfile | **No** — it needs `--build-arg` at build time |
| `$KEY` in a workflow `run:` script | **No** — reading is not providing |

### Renamed variables

`DB_HOST: ${POSTGRES_HOST}` renames a variable on the way into a container.
`DB_HOST` is treated as supplied exactly when `POSTGRES_HOST` is — not
unconditionally, and not never.

This resolves to a fixed point, so chains work:

```yaml
A: ${B}     # A is fine, because…
B: ${C}     # …B is fine, because…
C: value    # …C has a value
```

A cycle simply stops making progress and stays unresolved rather than hanging.

### What counts as *using* a variable

- Application code reading it (`os.Getenv`, `process.env.X`, …)
- A container or job receiving it — the code that reads it may live inside the
  image, where EnvGraph cannot see
- A Dockerfile line reading it, as in `ENV PATH=$APP_HOME/bin`

### Shell locals are not variables

In a workflow `run:` block, a name the script assigns to itself is not
configuration:

```yaml
run: |
  count=$(ls | wc -l)   # `count` is a shell local
  for item in *; do     # so is `item`
    echo "$item $count"
  done
  echo "$DATABASE_URL"  # this one is configuration
```

Assignments, `for` loops and `read` are all recognised.

---

## Configuration

A first run on a real project reports noise: a Dockerfile extending `PATH` looks
like an unused variable, committed fixtures look like broken configuration. Drop
an `.envgraph.yml` in the project root to fix that once instead of typing flags
every time.

```yaml
# Directory names to skip.
exclude:
  - examples
  - testdata

# Variables to drop entirely. Wildcards allowed.
ignore:
  - OLD_API_KEY
  - "VITE_*"

# Ignore variables the shell, OS or CI runner sets. On by default.
systemVariables: true
```

An ignored variable disappears from the report **and** the graph, so `scan`,
`check`, `export` and the viewer all agree. Only `explain` will still show it,
and it says so when it does.

`systemVariables` covers:

```
CI  EDITOR  GOPATH  GOROOT  HOME  HOSTNAME  LANG  LC_ALL  LOGNAME  NO_COLOR
OLDPWD  PATH  PWD  SHELL  SHLVL  TERM  TMPDIR  TZ  USER
ACTIONS_*  GITHUB_*  RUNNER_*
```

### Shared flags

| Flag | Effect |
| --- | --- |
| `--exclude <dir>` | Skip a directory name, on top of the config |
| `--ignore <name>` | Drop variables; globs allowed; adds to the config |
| `--include-tests` | Count usage in test files |
| `--config <file>` | Use a specific config file |
| `--no-config` | Ignore the config file *and* the system-variable defaults |

Flags layer on top of the file rather than replacing it. `--no-config` is the
exception: it turns everything off, which is the way to see the unfiltered truth.

EnvGraph ships [its own config](../.envgraph.yml) — the example projects contain
deliberately broken configuration, so scanning them from the repository root
would report those fixtures as real problems.

---

## Using it in CI

```yaml
- name: Check configuration
  run: |
    go install github.com/PeacexF/EnvGraph/cmd/envgraph@latest
    envgraph check .
```

`check` exits `1` on a missing variable, which fails the step. Add `--strict` to
fail on unused ones too.

On a project that already has findings, start by putting them in `ignore` and
removing them as you fix them — a check that fails on day one gets switched off
on day two.

---

## Secrets

`.env` files hold credentials, so values are **never** shown unless you ask:

- `scan` prints names and locations, not values
- `scan -f json` and `export` redact values
- `serve` does not send them to the browser
- `--show-values` opts in, per command

`serve` binds to `127.0.0.1` for the same reason.

What EnvGraph *does* show is that a value exists and where it is written. That is
usually what you wanted; a variable name and a file path are not the secret.

---

## When it gets something wrong

**A variable is reported missing but the app works.** Something supplies it that
EnvGraph cannot see — a secrets manager, a config framework reading struct tags,
a platform's environment settings. Add it to `ignore`.

**A variable is reported unused but is definitely read.** Most likely it is read
through a config library rather than a direct `os.Getenv`-style call. Framework
loaders are not parsed yet.

**Nothing is found at all.** Check that the path is right and that your files
are not inside a skipped directory. `--no-config` also rules out an over-broad
`exclude`.

**Test files.** Usage inside tests does not count by default, so a variable only
read in tests reports as unused. `--include-tests` changes that.

If the tool is wrong in a way that is not on this list, that is worth an issue —
false positives are the thing most likely to make it useless.
