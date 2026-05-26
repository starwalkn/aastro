package main

import "github.com/spf13/cobra"

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Generate and manage Aastro plugins",
}

func init() {
	rootCmd.AddCommand(pluginCmd)
}
