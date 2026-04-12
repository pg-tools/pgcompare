# pgcompare
A local CLI tool that automatically benchmarks and compares PostgreSQL query performance before and after any optimization — schema changes, new indexes, or query rewrites.

## Installation

### Release Links
- Latest release: https://github.com/pg-tools/pgcompare/releases/latest

### Linux/macOS via install script

Install latest release to `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh -s -- -v v0.1.0
```

Install to a custom directory:

```bash
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh -s -- -b /usr/local/bin
```

### Homebrew (macOS)

```bash
brew tap pg-tools/tap
brew install pg-tools/tap/pgcompare
```

## Verify Installation

```bash
pgcompare --help
```
