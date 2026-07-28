# Contributing

> Thanks for considering contributing to EnvGraph

All contributions are welcome: bug fixes, new features, documentation improvements, refactoring, performance optimizations, or just reporting issues.

## Pull Requests

* Keep PRs focused on one change.
* Write clear commit messages.
* Update documentation if needed.
* Add tests for new functionality.
* Test your changes before submitting.

If you're planning a large feature, consider opening an issue first so we can discuss it.

## Understanding the codebase

[docs/architecture.md](docs/architecture.md) covers the four stages, the data
model every parser speaks, and a step-by-step recipe for adding support for a
new configuration format. Start there before a first change.

## Running the checks

CI runs the same things you can run locally:

```bash
gofmt -l ./cmd ./internal ./tests ./web   # must print nothing
go vet ./...
go test -race ./...
```

Tests are black-box: they live in `package foo_test` and exercise only the
exported API. If something is hard to test that way, that usually points at
the API rather than the test.

## Coding Style

* Keep it simple.
* Prefer readable code over clever code.
* Avoid unnecessary dependencies.
* Stay consistent with the existing codebase.
* Keep the project focused on configuration flow analysis.
* Avoid adding support for new languages or formats without discussion first.

## Issues

Found a bug or have an idea?

Open an issue with as much detail as possible. Feature requests, improvements, and discussions are always welcome.

## Be Respectful

Please keep discussions friendly and constructive.

Thanks for helping improve EnvGraph.