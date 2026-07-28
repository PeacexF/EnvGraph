# EnvGraph documentation

| Document | For |
| --- | --- |
| [Guide](guide.md) | Using EnvGraph: commands, configuration, and how it decides what is missing |
| [Architecture](architecture.md) | How it works inside, and how to add a new configuration source |

The [README](../README.md) is the short version. These two go further.

## The short version

EnvGraph reads a project's configuration files and source code, then reports
where every environment variable comes from and where it ends up.

```bash
go install github.com/PeacexF/EnvGraph/cmd/envgraph@latest

envgraph scan .                    # what configuration exists
envgraph explain DATABASE_URL      # trace one variable
envgraph check .                   # fail when something is missing
envgraph serve .                   # explore it in a browser
```

## Where to start

- Never used it: [Guide → First run](guide.md#first-run)
- Getting noisy results: [Guide → Configuration](guide.md#configuration)
- Wondering why something is "missing": [Guide → How resolution works](guide.md#how-resolution-works)
- Adding support for a new file format: [Architecture → Adding a source](architecture.md#adding-a-source)
