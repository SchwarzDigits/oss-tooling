# oss-tooling

Tooling for the Schwarz Digits open-source compliance program.

This repository contains the command-line tools and GitHub Actions workflows
that we use to maintain transparency, license compliance, and security
hygiene across our open-source organizations on GitHub
(`SchwarzDigits`, `SchwarzIT`, `stackitcloud`).

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

The tool is primarily run via the GitHub Actions workflows defined under
`.github/workflows/`. Local execution is supported for development.

```bash
go build -o osstool ./cmd/osstool
./osstool inventory --orgs SchwarzDigits,SchwarzIT,stackitcloud
```

## Contributing

Contributions are welcome. Please see [CONTRIBUTING.md](https://github.com/SchwarzDigits/.github/blob/main/CONTRIBUTING.md)
in our `.github` repository for general guidelines, including our
Contributor License Agreement.

## License

Apache License 2.0. See [LICENSE](./LICENSE).
