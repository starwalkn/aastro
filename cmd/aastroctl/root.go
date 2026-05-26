package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version   string
	commit    string
	buildDate string
)

var rootCmd = &cobra.Command{
	Use:           "aastroctl",
	Short:         "Aastro control and developer tool",
	Long:          "aastroctl is a companion tool for the Aastro API gateway. It manages a running daemon and generates plugin scaffolding.",
	SilenceUsage:  true,
	SilenceErrors: false,
	Version:       versionString(),
}

func init() {
	rootCmd.SetVersionTemplate("aastroctl/{{.Version}}\n")
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
}

func versionString() string {
	return fmt.Sprintf(
		"%s (commit %s, built %s, %s %s/%s)",
		valueOr(version, "dev"),
		valueOr(commit, "unknown"),
		valueOr(buildDate, "unknown"),
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	)
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}

	return s
}
