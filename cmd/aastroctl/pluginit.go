package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"text/template"

	"github.com/spf13/cobra"
)

//go:embed templates/*.tmpl
var pluginTemplates embed.FS

type pluginInitFlags struct {
	pluginType  string
	name        string
	description string
	author      string
	output      string
}

func init() {
	flags := &pluginInitFlags{}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a new plugin or middleware skeleton",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPluginInit(*flags)
		},
	}

	cmd.Flags().StringVar(&flags.pluginType, "type", "", "plugin type: request, response, middleware")
	cmd.Flags().StringVar(&flags.name, "name", "", "plugin name (required)")
	cmd.Flags().StringVar(&flags.description, "description", "", "plugin description")
	cmd.Flags().StringVar(&flags.author, "author", "", "plugin author")
	cmd.Flags().StringVar(&flags.output, "out", "", "output file path (default: <name>.go in current directory)")

	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("name")

	pluginCmd.AddCommand(cmd)
}

func runPluginInit(f pluginInitFlags) error {
	tmplName, err := templateNameForType(f.pluginType)
	if err != nil {
		return err
	}

	out := f.output
	if out == "" {
		out = f.name + ".go"
	}

	if _, err = os.Stat(out); err == nil {
		return fmt.Errorf("file already exists: %s", out)
	}

	tmpl, err := template.ParseFS(pluginTemplates, "templates/"+tmplName)
	if err != nil {
		return fmt.Errorf("load template: %w", err)
	}

	data := struct {
		Name        string
		Description string
		Author      string
	}{
		Name:        f.name,
		Description: f.description,
		Author:      f.author,
	}

	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("format generated code: %w", err)
	}

	if err = os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if err = os.WriteFile(out, formatted, 0o600); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Fprintf(os.Stderr, "created %s\n", out)

	return nil
}

func templateNameForType(t string) (string, error) {
	switch t {
	case "request":
		return "request_plugin.go.tmpl", nil
	case "response":
		return "response_plugin.go.tmpl", nil
	case "middleware":
		return "middleware.go.tmpl", nil
	default:
		return "", fmt.Errorf("unknown plugin type: %q (must be request, response, middleware)", t)
	}
}
