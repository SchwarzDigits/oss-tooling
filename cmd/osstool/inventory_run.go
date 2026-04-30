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

	gh "github.com/SchwarzDigits/oss-tooling/internal/github"
	"github.com/SchwarzDigits/oss-tooling/internal/inventory"
)

type runFlags struct {
	orgs        []string
	output      string
	concurrency int
	exclude     []string
}

func newInventoryRunCmd() *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Collect repository metadata for one or more GitHub organizations",
		Long: `Enumerate non-archived repositories in the given organizations,
collect compliance metadata, and write per-org JSON files to the output directory.

Requires the GITHUB_TOKEN environment variable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(f.orgs) == 0 {
				return errors.New("--orgs is required")
			}
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
			}

			start := time.Now()
			summary, err := c.Run(ctx, f.orgs)
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

	cmd.Flags().StringSliceVar(&f.orgs, "orgs", nil, "comma-separated list of GitHub organizations (required)")
	cmd.Flags().StringVar(&f.output, "output", "./output", "output directory for per-org JSON files")
	cmd.Flags().IntVar(&f.concurrency, "concurrency", 5, "number of repositories enriched in parallel (1..20)")
	cmd.Flags().StringSliceVar(&f.exclude, "exclude", []string{".github"},
		"comma-separated repository names to skip (case-insensitive, matches Name not full_name)")
	_ = cmd.MarkFlagRequired("orgs")
	return cmd
}
