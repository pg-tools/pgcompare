# Installation

## Release Links

- Latest release: https://github.com/pg-tools/pgcompare/releases/latest

## Linux/macOS via install script

Install latest release to `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh -s -- -v v1.0.1
```

Install to a custom directory:

```bash
curl -fsSL https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.sh | sh -s -- -b /usr/local/bin
```

## Windows via PowerShell

Install latest release to `$HOME\.local\bin`:

```powershell
irm https://raw.githubusercontent.com/pg-tools/pgcompare/main/install.ps1 | iex
```

Install a specific version:

```powershell
.\install.ps1 -Version v1.0.1
```

Install to a custom directory:

```powershell
.\install.ps1 -BinDir C:\tools
```

## Homebrew (macOS)

```bash
brew tap pg-tools/tap
brew install pg-tools/tap/pgcompare
```

## Verify Installation

```bash
pgcompare --help
```
