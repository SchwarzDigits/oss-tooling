package main

import "github.com/spf13/cobra"

func newInventoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "Repository inventory commands",
		Long:  `Enumerate org repositories ("run"), generate a Markdown report ("report"), or compare two snapshots ("diff").`,
	}
	cmd.AddCommand(newInventoryRunCmd())
	cmd.AddCommand(newInventoryReportCmd())
	cmd.AddCommand(newInventoryDiffCmd())
	return cmd
}
