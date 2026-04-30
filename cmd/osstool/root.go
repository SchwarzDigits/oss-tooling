package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "osstool",
		Short: "OSS compliance tooling for Schwarz Digits",
		Long: `osstool collects compliance signals across our open-source organizations
on GitHub. The first subcommand is "inventory", which enumerates repositories
and writes structured metadata plus a Markdown summary report.`,
		SilenceUsage: true,
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cmd.AddCommand(newInventoryCmd())
	return cmd
}
