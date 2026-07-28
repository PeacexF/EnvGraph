# EnvGraph

> Visualize how configuration flows through your application.

EnvGraph is a developer tool that analyzes project configuration and generates an interactive graph showing where environment variables come from and where they are used.

Modern applications spread configuration across many places:

- `.env` files
- Docker Compose
- source code
- deployment files
- CI/CD configuration

EnvGraph helps answer:

- Where does this environment variable come from?
- Where is it passed?
- Which part of the application uses it?
- Which configuration values are missing or unused?

---

## Example

Instead of manually searching:

```bash
grep -R DATABASE_URL .
````

EnvGraph creates a visual flow:

```
.env

DATABASE_URL
      |
      v

docker-compose.yml

      |
      v

api container

      |
      v

config/database.go

      |
      v

PostgreSQL connection
```

---

# Features

## Configuration Flow Analysis

Detect relationships between:

* configuration files
* containers
* environment variables
* application code

---

## Interactive Graph

Explore your project's configuration structure visually.

Understand:

* sources
* consumers
* dependencies
* configuration flow

---

## Missing Configuration Detection

Find variables that are used but not provided.

Example:

```
JWT_SECRET

Used in:
auth/config.go

Source:
missing
```

---

## Unused Configuration Detection

Identify configuration values that exist but are never used.

Example:

```
OLD_API_KEY

Defined in:
.env

Usage:
none
```

---

# Supported Sources

Currently supported:

| Source                    | Status  |
| ------------------------- | ------- |
| `.env` files              | Planned |
| Docker Compose            | Planned |
| Go environment access     | Planned |
| Python environment access | Planned |

More sources will be added gradually.

---

# Installation

> Installation instructions will be added once the first release is available.

---

# Usage

Example:

```bash
envgraph scan .
```

Generate a configuration graph:

```bash
envgraph serve
```

Open the visualization in your browser.

---

# Architecture

EnvGraph consists of three main parts:

```
                CLI

                 |

              Scanner

                 |

            Graph Engine

                 |

             Web Viewer
```

### Scanner

Finds configuration sources and consumers.

### Graph Engine

Builds relationships between configuration nodes.

### Web Viewer

Displays the configuration flow interactively.

---

# Why EnvGraph?

Configuration is one of the hidden complexities of modern applications.

A project may have:

```
.env
docker-compose.yml
application config
source code
CI variables
```

but no clear overview of how values move through the system.

EnvGraph provides that missing layer of visibility.

---

# Contributing

Contributions are welcome.

Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting changes.

---

# License

[MIT](LICENSE)
