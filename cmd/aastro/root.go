package main

import (
	"os"

	"github.com/spf13/cobra"
)

const fallbackConfigPath = "/etc/aastro/config.yaml"

var cfgPath string

var rootCmd = &cobra.Command{
	Use:   "aastro",
	Short: "Aastro API Gateway",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.SilenceUsage = true

	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
	rootCmd.PersistentFlags().StringVar(
		&cfgPath,
		"config",
		"",
		"Path to configuration file (env AASTRO_CONFIG)",
	)
}
