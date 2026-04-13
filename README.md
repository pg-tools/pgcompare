# pgcompare

[![license](https://img.shields.io/github/license/pg-tools/pgcompare)](https://github.com/pg-tools/pgcompare/blob/main/LICENSE)
[![release](https://img.shields.io/github/v/release/pg-tools/pgcompare)](https://github.com/pg-tools/pgcompare/releases/latest)
[![Go](https://github.com/pg-tools/pgcompare/actions/workflows/ci.yml/badge.svg)](https://github.com/pg-tools/pgcompare/actions/workflows/ci.yml)
[![go report](https://goreportcard.com/badge/github.com/pg-tools/pgcompare)](https://goreportcard.com/report/github.com/pg-tools/pgcompare)
[![codecov](https://codecov.io/gh/pg-tools/pgcompare/branch/main/graph/badge.svg)](https://codecov.io/gh/pg-tools/pgcompare)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey)](https://github.com/pg-tools/pgcompare/releases/latest)

`pgcompare` is a local CLI tool for comparing PostgreSQL query performance before and after an optimization and generating a single HTML report.

For Russian documentation, see [RU.md](./RU.md).

## Installation

```bash
# Linux/macOS
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.ps1 | iex
```

```bash
# macOS (Homebrew)
brew tap pg-tools/tap
brew install pg-tools/tap/pgcompare
```

For more options (custom version, custom directory), see [INSTALL.md](./INSTALL.md).

## What pgcompare Does

`pgcompare` runs the same benchmark flow twice:

1. Load `pgcompare.yaml` and `.env` from the same project directory.
2. Prepare the `before` state with the configured migration env var.
3. Benchmark the `before` queries and capture `EXPLAIN ANALYZE` plans.
4. Recreate the environment for the `after` state.
5. Benchmark the `after` queries and capture plans again.
6. Generate one HTML comparison report.

This is intended for comparing the effect of:

- schema changes
- new or changed indexes
- rewritten SQL
- migration changes
- changes in seeded data

## Requirements

- Docker with either `docker compose` v2 or `docker-compose` v1 in `PATH`
- PostgreSQL reachable from the host on `localhost:$POSTGRES_PORT`
- A project directory containing `.env`, `pgcompare.yaml`, and the SQL files referenced by the config

If you build from source, use Go `1.25`.

## Project Layout

`pgcompare` treats the directory that contains `pgcompare.yaml` as the project root. It loads `.env` from there, runs external commands there, and writes the default report there.

Recommended layout:

```text
student-project/
├── .env
├── docker-compose.yml
├── pgcompare.yaml
├── queries_before.sql
└── queries_after.sql
```

Common `.env` keys:

```dotenv
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=app
POSTGRES_PORT=5432
MIGRATION_VERSION=0
```

`POSTGRES_PORT` is optional and defaults to `5432`. If you keep the default migration env var name, use `MIGRATION_VERSION`. If you override it with `migration.env_var`, use the same custom key in `.env`.

## Configuration

Example `pgcompare.yaml`:

```yaml
migration:
  env_var: MIGRATION_VERSION
  before_version: "3"
  after_version: "5"

setup:
  command: "$DOCKER_COMPOSE up -d postgres && $DOCKER_COMPOSE run --rm -T migrate && $DOCKER_COMPOSE run --rm -T seed"

benchmark:
  before_queries: queries_before.sql
  after_queries: queries_after.sql
  iterations: 100
  concurrency: 4

report:
  description:
    - query: find_active_users
      what: Replace a sequential scan with an index-backed lookup.
      changes: |
        CREATE INDEX idx_users_active_created_at
          ON users (active, created_at DESC);
      expected: Lower p95 latency and higher QPS for the query.
```

Notes on the setup command flags:

- `up -d postgres` — starts the PostgreSQL container explicitly before running migrations, ensuring the database is available
- `-T` — disables TTY allocation. `docker compose run` allocates a TTY by default, which fails when the command is launched from `pgcompare` (non-interactive context). Without `-T`, the setup command will exit with an error
- `--rm` — removes the container after it exits. Without this flag, each `pgcompare run` leaves behind stopped migrate/seed containers

How migration switching works:

`pgcompare` does not apply migrations by itself. It runs your `setup.command` twice and overrides the migration env var:

- first with `migration.before_version`
- then with `migration.after_version`

By default, the env var name is `MIGRATION_VERSION`. If needed, you can override it with `migration.env_var`.

Because of that, your migration workflow must explicitly use the same env var name. If your migration script or Docker service ignores that variable, both phases will prepare the same database state.

Typical setup:

`.env`

```dotenv
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=app
POSTGRES_PORT=5432
MIGRATION_VERSION=0
```

`docker-compose.yml`

```yaml
services:
  migrate:
    env_file:
      - .env
    environment:
      MIGRATION_VERSION: ${MIGRATION_VERSION}
    command: sh -c "./migrate up --to ${MIGRATION_VERSION}"
```

If you use a custom `migration.env_var`, the same name must be used in `.env`, in `docker-compose.yml`, and inside the migration command.

`MIGRATION_VERSION` in `.env` is useful as a default for manual local runs. During `pgcompare run`, it is overridden automatically for the `before` and `after` phases.

Configuration structure:

### `migration`

- `env_var`: optional env var name used for migration switching. Default: `MIGRATION_VERSION`
- `before_version`: value injected into the migration env var for the first phase
- `after_version`: value injected into the migration env var for the second phase

These two values define which schema state is compared. They should point to two valid migration states that your project can build successfully.

### `setup`

- `command`: shell command executed in the project directory

This command is responsible for preparing the database completely. In most projects it starts containers, applies migrations, and optionally seeds data. It should exit with status `0` only when PostgreSQL is actually ready for benchmark queries.

### `benchmark`

- `before_queries`: SQL file used for the `before` phase
- `after_queries`: SQL file used for the `after` phase
- `iterations`: total number of executions per query
- `concurrency`: number of parallel workers used for each benchmark

The query file paths are resolved relative to the directory that contains `pgcompare.yaml`.

### `report`

- `description`: optional list rendered at the top of the report

Each description entry may contain:

- `query`: query name shown as a label in the report
- `what`: short explanation of what was optimized
- `changes`: schema or SQL changes that were applied
- `expected`: expected effect of the optimization

`pgcompare` also injects `DOCKER_COMPOSE` into the setup command so the same command can work with both Compose v1 and v2.

## Query File Format

Each SQL file must contain named queries:

```sql
-- name: find_active_users
SELECT id, email
FROM users
WHERE active = true
ORDER BY created_at DESC
LIMIT 100;

-- name: count_orders_by_status
SELECT status, COUNT(*)
FROM orders
GROUP BY status;
```

Rules:

- both files must contain the same query names
- keep the queries in the same order in both files
- query names must be unique inside each file

## Report Contents

See [example_report.html](./example_report.html) for a complete example of the generated report.

The HTML report is designed to answer three practical questions:

1. What exactly was changed?
2. Did the queries become faster?
3. How did the execution plans change?

The top section of the report shows the optional optimization description from `report.description`. This is where you can explain the goal of the change, record the SQL or schema update, and state the expected outcome.

The summary section gives a compact before/after view for each query:

- p95 latency before and after
- p95 delta
- speedup
- QPS before and after
- QPS delta

The speedup badge is calculated from p95 latency:

```text
speedup = p95_before / p95_after
```

Each query then gets its own detailed card with:

- p50, p95, p99, min, max, mean, and standard deviation
- QPS and error rate
- percentage deltas between the `before` and `after` runs
- a short summary of detected plan changes
- rendered `before` and `after` query plans

If `report.description` is empty, the report shows a warning block at the top so it is immediately clear that the explanatory part is missing.

## Usage

After preparing `.env`, `pgcompare.yaml`, and the query files, run:

```bash
pgcompare run --config ./pgcompare.yaml
```

By default, the report is written to `report.html` next to the config file.

Write the report to a custom path:

```bash
pgcompare run --config ./pgcompare.yaml --out ./artifacts/report.html
```

Enable verbose logs:

```bash
pgcompare run --config ./pgcompare.yaml --verbose
```

Show command help:

```bash
pgcompare run --help
```

## Notes

- The CLI currently connects to PostgreSQL through `localhost`, using credentials from `.env`
- If `--out` is omitted, the default output file is `report.html` in the config directory
- The HTML report interface is currently in Russian
