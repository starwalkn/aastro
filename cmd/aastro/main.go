package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

func main() {
	os.Exit(run())
}

func run() int {
	flags, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "Try 'aastro --help' for more information.")
		return 1
	}

	switch {
	case flags.showVersionVerbose:
		printVersionVerbose(os.Stdout)
		return 0
	case flags.showVersion:
		printVersion(os.Stdout)
		return 0
	case flags.testDump:
		return runTest(resolveConfigPath(flags.configPath), true, flags.quiet)
	case flags.test:
		return runTest(resolveConfigPath(flags.configPath), false, flags.quiet)
	default:
		return runGateway(resolveConfigPath(flags.configPath))
	}
}

const fallbackConfigPath = "/etc/aastro/config.yaml"

func resolveConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}

	if env := os.Getenv("AASTRO_CONFIG"); env != "" {
		return env
	}

	return fallbackConfigPath
}
