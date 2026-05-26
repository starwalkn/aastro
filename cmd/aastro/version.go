package main

import (
	"fmt"
	"io"
	"runtime"
)

var (
	version   string
	commit    string
	buildDate string
)

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "aastro/%s\n", versionOrDev())
}

func printVersionVerbose(w io.Writer) {
	fmt.Fprintf(w, "aastro version: aastro/%s\n", versionOrDev())
	fmt.Fprintf(w, "built with:     %s (%s/%s)\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(w, "built at:       %s\n", valueOrUnknown(buildDate))
	fmt.Fprintf(w, "commit:         %s\n", valueOrUnknown(commit))
}

func versionOrDev() string {
	if version == "" {
		return "dev"
	}
	return version
}

func valueOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
