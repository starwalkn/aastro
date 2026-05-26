package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

type cliFlags struct {
	configPath         string
	test               bool
	testDump           bool
	quiet              bool
	showVersion        bool
	showVersionVerbose bool
}

func parseFlags(args []string) (*cliFlags, error) {
	fs := pflag.NewFlagSet("aastro", pflag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.SortFlags = false

	f := &cliFlags{}

	fs.StringVarP(&f.configPath, "config", "c", "", "configuration file path (env: AASTRO_CONFIG)")
	fs.BoolVarP(&f.test, "test", "t", false, "test configuration and exit")
	fs.BoolVarP(&f.testDump, "test-dump", "T", false, "test configuration, dump effective config to stdout, exit")
	fs.BoolVarP(&f.quiet, "quiet", "q", false, "suppress non-error output")
	fs.BoolVarP(&f.showVersion, "version", "v", false, "print version and exit")
	fs.BoolVarP(&f.showVersionVerbose, "version-verbose", "V", false, "print version with build details and exit")

	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "Usage: aastro [options]\n\nOptions:\n")
		fmt.Fprint(fs.Output(), fs.FlagUsages())
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "aastro: unexpected argument: %q\n", fs.Arg(0))
		return nil, errors.New("unexpected positional argument")
	}

	return f, nil
}
