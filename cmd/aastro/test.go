package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/starwalkn/aastro"
)

func runTest(cfgPath string, dump, quiet bool) int {
	cfg, err := aastro.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aastro: configuration file %s test failed\n", cfgPath)
		fmt.Fprintf(os.Stderr, "aastro: %v\n", err)
		return 2 //nolint:mnd // exit code
	}

	if !quiet {
		fmt.Fprintf(os.Stderr, "aastro: configuration file %s test is successful\n", cfgPath)
	}

	if dump {
		if err = writeDump(os.Stdout, &cfg, cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "aastro: dump failed: %v\n", err)
			return 1
		}
	}

	return 0
}

func writeDump(w io.Writer, cfg *aastro.Config, cfgPath string) error {
	header := fmt.Sprintf(
		"# configuration file %s test is successful\n# aastro/%s at %s\n#\n",
		cfgPath,
		versionOrDev(),
		time.Now().UTC().Format(time.RFC3339),
	)
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}

	_, err := cfg.WriteTo(w)

	return err
}
