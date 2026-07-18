package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/starwalkn/aastro"
	"github.com/starwalkn/aastro/internal/openapi"
)

const indent = 2

var openapiCmd = &cobra.Command{
	Use:   "openapi",
	Short: "OpenAPI tooling for Aastro configurations",
}

type openapiExportFlags struct {
	config     string
	output     string
	format     string
	oasVersion string
	servers    []string
	title      string
	apiVersion string
	extensions bool
}

func init() {
	rootCmd.AddCommand(openapiCmd)

	flags := &openapiExportFlags{}

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Generate an OpenAPI document from a gateway configuration",
		Long: "Loads the configuration through the same pipeline as the gateway " +
			"(defaults + validation), so a broken config fails here before any deploy.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOpenAPIExport(cmd, *flags)
		},
	}

	cmd.Flags().StringVarP(&flags.config, "config", "c", "aastro.yaml", "path to the gateway configuration")
	cmd.Flags().StringVarP(&flags.output, "out", "o", "-", "output file ('-' for stdout)")
	cmd.Flags().StringVar(&flags.format, "format", "", "yaml or json (default: by output extension, else yaml)")
	cmd.Flags().StringVar(&flags.oasVersion, "oas-version", "3.1", "OpenAPI version: 3.1 or 3.0")
	cmd.Flags().StringArrayVar(&flags.servers, "server", nil, "server URL for servers[] (repeatable)")
	cmd.Flags().StringVar(&flags.title, "title", "", "info.title (default: gateway service name)")
	cmd.Flags().StringVar(&flags.apiVersion, "api-version", "", "info.version (default: 0.0.0)")
	cmd.Flags().BoolVar(&flags.extensions, "extensions", false, "include x-aastro round-trip extensions")

	openapiCmd.AddCommand(cmd)
}

func runOpenAPIExport(cmd *cobra.Command, f openapiExportFlags) error {
	cfg, err := aastro.LoadConfig(f.config)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	doc, warnings, err := openapi.FromConfig(cfg, openapi.Options{
		OASVersion:       f.oasVersion,
		Title:            f.title,
		APIVersion:       f.apiVersion,
		Servers:          f.servers,
		Extensions:       f.extensions,
		GeneratorVersion: valueOr(version, "dev"),
	})
	if err != nil {
		return err
	}

	for _, w := range warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
	}

	data, err := marshalOpenAPIDoc(doc, resolveOpenAPIFormat(f.format, f.output))
	if err != nil {
		return err
	}

	if f.output == "" || f.output == "-" {
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}

	if err = os.WriteFile(f.output, data, 0o600); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "created %s\n", f.output)

	return nil
}

func resolveOpenAPIFormat(format, outPath string) string {
	if format != "" {
		return strings.ToLower(format)
	}

	if strings.ToLower(filepath.Ext(outPath)) == ".json" {
		return "json"
	}

	return "yaml"
}

func marshalOpenAPIDoc(doc *openapi.Document, format string) ([]byte, error) {
	switch format {
	case "yaml":
		var sb strings.Builder

		enc := yaml.NewEncoder(&sb)
		enc.SetIndent(indent)

		if err := enc.Encode(doc); err != nil {
			return nil, fmt.Errorf("marshal yaml: %w", err)
		}

		if err := enc.Close(); err != nil {
			return nil, fmt.Errorf("close yaml encoder: %w", err)
		}

		return []byte(sb.String()), nil
	case "json":
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal json: %w", err)
		}

		return append(data, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported format %q (must be yaml or json)", format)
	}
}
