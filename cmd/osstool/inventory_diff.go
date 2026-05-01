package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SchwarzDigits/oss-tooling/internal/inventory"
)

type diffFlags struct {
	from   string
	to     string
	output string
}

func newInventoryDiffCmd() *cobra.Command {
	var f diffFlags
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare two inventory snapshots and emit a Markdown summary of changes",
		Long: `Compare two snapshots of per-org JSON data and write a Markdown summary
of the differences. --from and --to each point at either a single org JSON
file or a directory of them (e.g., output/2026-04-30 or output/latest).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.from == "" || f.to == "" {
				return errors.New("--from and --to are required")
			}

			fromRepos, fromDate, err := inventory.LoadSnapshot(f.from)
			if err != nil {
				return fmt.Errorf("load --from: %w", err)
			}
			toRepos, toDate, err := inventory.LoadSnapshot(f.to)
			if err != nil {
				return fmt.Errorf("load --to: %w", err)
			}

			d := inventory.ComputeDiff(fromRepos, toRepos, fromDate, toDate)
			body := inventory.RenderDiffMarkdown(d)

			if f.output == "" {
				_, err := fmt.Fprint(cmd.OutOrStdout(), body)
				return err
			}

			dir := filepath.Dir(f.output)
			if dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("mkdir %q: %w", dir, err)
				}
			}
			if err := os.WriteFile(f.output, []byte(body), 0o644); err != nil {
				return fmt.Errorf("write %q: %w", f.output, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "diff written: %s\n", f.output)
			return nil
		},
	}

	cmd.Flags().StringVar(&f.from, "from", "", "previous snapshot (file or directory) (required)")
	cmd.Flags().StringVar(&f.to, "to", "", "newer snapshot (file or directory) (required)")
	cmd.Flags().StringVar(&f.output, "output", "", "path to write Markdown (default stdout)")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}
