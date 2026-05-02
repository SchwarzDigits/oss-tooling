package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/SchwarzDigits/oss-tooling/internal/config"
	gh "github.com/SchwarzDigits/oss-tooling/internal/github"
	"github.com/SchwarzDigits/oss-tooling/internal/inventory"
)

type runFlags struct {
	orgs        []string
	output      string
	concurrency int
	exclude     []string
	configPath  string
}

func newInventoryRunCmd() *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Collect repository metadata for one or more GitHub organizations",
		Long: `Enumerate non-archived repositories in the given organizations,
collect compliance metadata, and write per-org JSON files to the output directory.

Orgs are read from --orgs (CLI), or, if that flag is empty, from the
"orgs" key of the YAML config at --config (default config/inventory.yml).
The same config file lists repos to exclude from collection.

Requires the GITHUB_TOKEN environment variable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			token := os.Getenv("GITHUB_TOKEN")
			if token == "" {
				return errors.New("GITHUB_TOKEN is required")
			}
			if f.concurrency < 1 {
				f.concurrency = 1
			}
			if f.concurrency > 20 {
				f.concurrency = 20
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logger := slog.Default()
			cfg := config.LoadInventoryOrEmpty(f.configPath, logger)

			orgs := f.orgs
			if len(orgs) == 0 {
				orgs = cfg.Orgs
			}
			if len(orgs) == 0 {
				return errors.New("no orgs to scan: pass --orgs or list orgs in " + f.configPath)
			}

			clients, err := gh.NewClients(ctx, token, logger)
			if err != nil {
				return fmt.Errorf("github clients: %w", err)
			}

			c := &inventory.Collector{
				Clients:     clients,
				Logger:      logger,
				Concurrency: f.concurrency,
				OutputDir:   f.output,
				Exclude:     f.exclude,
				IsExcluded:  cfg.Excludes.IsExcluded,
			}

			start := time.Now()
			summary, err := c.Run(ctx, orgs)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(),
				"inventory complete: orgs=%d repos=%d with_license=%d missing_license=%d missing_readme=%d missing_security=%d uses_compliance_workflow=%d stale_365d=%d duration=%s\n",
				summary.Orgs,
				summary.TotalRepos,
				summary.WithLicense,
				summary.WithoutLicense,
				summary.MissingReadme,
				summary.MissingSecurity,
				summary.UsesComplianceWorkflow,
				summary.StaleNoCommits365d,
				time.Since(start).Round(time.Second),
			)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&f.orgs, "orgs", nil,
		"comma-separated GitHub organizations to scan (overrides config)")
	cmd.Flags().StringVar(&f.output, "output", "./output", "output directory for per-org JSON files")
	cmd.Flags().IntVar(&f.concurrency, "concurrency", 5, "number of repositories enriched in parallel (1..20)")
	cmd.Flags().StringSliceVar(&f.exclude, "exclude", []string{".github"},
		"comma-separated repository names to skip (case-insensitive, matches Name not full_name)")
	cmd.Flags().StringVar(&f.configPath, "config", "config/inventory.yml",
		"path to inventory YAML config (orgs + excludes); missing file is logged and ignored")
	return cmd
}
