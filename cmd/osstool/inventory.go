package main

import "github.com/spf13/cobra"

func newInventoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "Repository inventory commands",
		Long:  `Enumerate org repositories ("run") and generate a Markdown report ("report").`,
	}
	cmd.AddCommand(newInventoryRunCmd())
	cmd.AddCommand(newInventoryReportCmd())
	return cmd
}
