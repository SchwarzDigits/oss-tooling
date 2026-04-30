# oss-tooling

Tooling for the Schwarz Digits open-source compliance program.

This repository contains the command-line tools and GitHub Actions workflows
that we use to maintain transparency, license compliance, and security
hygiene across our open-source organizations on GitHub
(`SchwarzDigits`, `SchwarzIT`).

## Status

Early development. The first focus is a repository inventory tool that
enumerates our open-source repositories and records their metadata,
license status, and key compliance signals.

## Components

| Component | Description |
|-----------|-------------|
| `cmd/osstool` | Command-line entry point |
| `internal/inventory` | Repository enumeration and metadata collection |
| `internal/github` | GitHub API client (GraphQL-first) |

More components will be added over time.

## Usage

The tool is intended to run via GitHub Actions for production runs and
locally for development. Local execution requires Go 1.23+ and a GitHub
personal access token in `GITHUB_TOKEN`.

```bash
make build
export GITHUB_TOKEN=ghp_xxx

# Collect repositories from one or more orgs into ./output
./bin/osstool inventory run --orgs SchwarzDigits,SchwarzIT

# Generate a Markdown summary from the latest output
./bin/osstool inventory report
```

Output layout:

```
output/
├── 2026-04-30/
│   ├── SchwarzDigits.json
│   └── SchwarzIT.json
└── latest/
    ├── SchwarzDigits.json
    └── SchwarzIT.json
output/summary.md   # produced by `inventory report`
```

### Make targets

| Target | Description |
|---|---|
| `make build` | Build the binary at `./bin/osstool` |
| `make test` | Run the Go test suite |
| `make vet` | Run `go vet ./...` |
| `make lint` | Run `golangci-lint run` (requires golangci-lint installed locally) |
| `make run-inventory` | Convenience wrapper around `inventory run` with default orgs |
| `make clean` | Remove `bin/` and `output/` |

## Contributing

Contributions are welcome. Please see [CONTRIBUTING.md](https://github.com/SchwarzDigits/.github/blob/main/CONTRIBUTING.md)
in our `.github` repository for general guidelines, including our
Contributor License Agreement.

## License

Apache License 2.0. See [LICENSE](./LICENSE).
