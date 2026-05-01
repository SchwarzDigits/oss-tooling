package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SchwarzDigits/oss-tooling/internal/inventory"
)

type reportFlags struct {
	input    string
	output   string
	diffFrom string
}

func newInventoryReportCmd() *cobra.Command {
	var f reportFlags
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate a Markdown summary from previously collected per-org JSON files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := inventory.GenerateReport(f.input, f.output, f.diffFrom); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "report written: %s\n", f.output)
			return nil
		},
	}

	cmd.Flags().StringVar(&f.input, "input", "./output/latest", "directory containing per-org JSON files")
	cmd.Flags().StringVar(&f.output, "output", "./output/summary.md", "path to write the Markdown report")
	cmd.Flags().StringVar(&f.diffFrom, "diff-from", "",
		"optional: previous snapshot (file or directory) to diff against — adds a 'Changes since previous snapshot' section")
	return cmd
}
