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

# Collect repositories using the orgs listed in config/inventory.yml
./bin/osstool inventory run

# Or override the config and pass orgs on the command line
./bin/osstool inventory run --orgs SchwarzDigits,SchwarzIT

# Generate a Markdown summary from the latest output
./bin/osstool inventory report

# Generate a report that also calls out what changed since a previous
# snapshot (file or directory of per-org JSON files):
./bin/osstool inventory report \
  --input ./output/latest \
  --diff-from ./output/2026-04-30 \
  --output ./output/summary.md

# Compare two snapshots directly and emit a Markdown diff
./bin/osstool inventory diff \
  --from ./output/2026-04-30 \
  --to ./output/latest \
  --output ./output/diff.md
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

The per-repo JSON schema gained fields for the latest compliance-workflow
run (`last_compliance_run_*`) and a likely-owner hint (`likely_owner`,
`likely_owner_source`). All new fields are additive and `omitempty`, so
older snapshots remain readable; missing fields default to zero values.

### Configuration

`config/inventory.yml` drives both which orgs are scanned and which
repos are excluded:

```yaml
orgs:
  - SchwarzDigits
  - SchwarzIT
excludes:
  - "*/.github"                      # any-org pattern
  - SchwarzDigits/oss-compliance     # exact org/name pattern
```

`orgs` are the default list scanned by `osstool inventory run`; the
`--orgs` CLI flag overrides them. `excludes` lists repos that aren't
meaningful subjects of compliance reporting (the central workflow
definition, data-only repos, the org `.github` repo). Two pattern forms
are supported: `<org>/<name>` for an exact match and `*/<name>` to match
a name across any org. Comparisons are case-sensitive.

Point at a different config with `--config <path>`. The CLI works
without the file present — a missing config is logged and treated as
"no defaults", in which case `--orgs` becomes mandatory.

Excluded repos are dropped at collection time, so they never appear in
the per-org JSON snapshots or the report. Snapshots taken before a repo
was added to the excludes will still contain it; the diff sub-command
will surface the one-time disappearance as a "Repositories removed" entry.

### Report sections

Every repo is classified into one of three lifecycle statuses —
**active** (pushed in the last 12 months), **stale** (older), or
**archived** — and that vocabulary is reused across the per-org table,
the **Compliance: Migration Priority** section, and the Archive
Candidates section. The report's **Status definitions** footer at the
end of the report spells out the rules in one place; section
methodology hints reference the footer rather than restating them.

Status header percentages use the active count as the denominator —
stale and archived repos can't realistically be onboarded, so including
them would understate adoption progress. The license-compliance bullet
also surfaces the overall ratio (across all statuses), since legal
obligation isn't excused by stale status.

The "Likely owner" column attributes a repo first to a CODEOWNERS file
(suffix **(CO)**) and otherwise to the dominant non-bot author in the
last 100 commits on the default branch, with a fallback to commits
101-200 if the first 100 are bot-only (suffix **(committer)**). The
underlying `LikelyOwnerSource` field on the JSON snapshot is one of
`codeowners`, `top_committer_recent`, or empty (renaming the older
`top_committer_90d` value — readers handling old snapshots can treat
any `top_committer_*` value as the same source class).

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
