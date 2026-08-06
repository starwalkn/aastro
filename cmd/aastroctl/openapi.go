package main

import (
	"bytes"
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

func init() {
	rootCmd.AddCommand(openapiCmd)
	openapiCmd.AddCommand(newOpenAPIExportCmd(), newOpenAPIImportCmd())
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

func newOpenAPIExportCmd() *cobra.Command {
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

	return cmd
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

	printWarnings(cmd, warnings)

	data, err := marshalOpenAPIDoc(doc, resolveOpenAPIFormat(f.format, f.output))
	if err != nil {
		return err
	}

	return writeOutput(cmd, f.output, data)
}

type openapiImportFlags struct {
	input       string
	output      string
	defaultHost string
	mode        string
	serverPort  int
	adminPort   int
	force       bool
}

func newOpenAPIImportCmd() *cobra.Command {
	flags := &openapiImportFlags{}

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Generate a gateway configuration from an OpenAPI document",
		Long: "Documents produced by 'openapi export --extensions' are reconstructed losslessly " +
			"(except middleware configs, which are never stored in specs). Foreign documents are " +
			"scaffolded as single-upstream flows. The result passes gateway validation but should " +
			"be reviewed: check warnings for placeholders and inferred settings.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOpenAPIImport(cmd, *flags)
		},
	}

	cmd.Flags().StringVarP(&flags.input, "in", "i", "", "OpenAPI document to import (yaml or json)")
	cmd.Flags().StringVarP(&flags.output, "out", "o", "-", "output configuration file ('-' for stdout)")
	cmd.Flags().StringVar(&flags.defaultHost, "default-host", "", "upstream host for scaffolded flows (default: servers[0] from the document)")
	cmd.Flags().StringVar(&flags.mode, "mode", "envelope", "flow shape for scaffolded operations: envelope or passthrough")
	cmd.Flags().IntVar(&flags.serverPort, "server-port", 0, "gateway data port for the generated config (default: 7805)")
	cmd.Flags().IntVar(&flags.adminPort, "admin-port", 0, "gateway admin port for the generated config (default: 9090)")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite the output file if it exists")

	_ = cmd.MarkFlagRequired("in")

	return cmd
}

func runOpenAPIImport(cmd *cobra.Command, f openapiImportFlags) error {
	if f.output != "-" && !f.force {
		if _, err := os.Stat(f.output); err == nil {
			return fmt.Errorf("file already exists: %s (use --force to overwrite)", f.output)
		}
	}

	data, err := os.ReadFile(f.input)
	if err != nil {
		return fmt.Errorf("read document: %w", err)
	}

	var doc openapi.Document
	if err = yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse OpenAPI document: %w", err)
	}

	cfg, warnings, err := openapi.ToConfig(&doc, openapi.ImportOptions{
		DefaultHost: f.defaultHost,
		Mode:        f.mode,
		ServerPort:  f.serverPort,
		AdminPort:   f.adminPort,
	})
	if err != nil {
		return err
	}

	printWarnings(cmd, warnings)

	clone, err := cloneConfig(&cfg)
	if err != nil {
		return err
	}

	if err = aastro.ValidateConfig(clone); err != nil {
		return fmt.Errorf("generated configuration failed validation (this is a bug, please report it):\n%w", err)
	}

	out, err := marshalConfigPruned(&cfg)
	if err != nil {
		return err
	}

	return writeOutput(cmd, f.output, out)
}

func cloneConfig(cfg *aastro.Config) (*aastro.Config, error) {
	raw, err := cfg.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	var clone aastro.Config
	if err = yaml.Unmarshal(raw, &clone); err != nil {
		return nil, fmt.Errorf("clone config: %w", err)
	}

	return &clone, nil
}

func marshalConfigPruned(cfg *aastro.Config) ([]byte, error) {
	raw, err := cfg.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	var node yaml.Node
	if err = yaml.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("reparse config: %w", err)
	}

	pruneZeroNodes(&node)

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)

	if err = enc.Encode(&node); err != nil {
		return nil, fmt.Errorf("encode pruned config: %w", err)
	}

	if err = enc.Close(); err != nil {
		return nil, fmt.Errorf("close yaml encoder: %w", err)
	}

	return buf.Bytes(), nil
}

// pruneZeroNodes removes zero-valued mapping entries recursively.
// Safe here because the importer never emits a zero value with non-default
// meaning, and LoadConfig re-applies defaults on the way back in.
func pruneZeroNodes(n *yaml.Node) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			pruneZeroNodes(c)
		}
	case yaml.MappingNode:
		kept := make([]*yaml.Node, 0, len(n.Content))

		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			pruneZeroNodes(value)

			if isZeroNode(value) {
				continue
			}

			kept = append(kept, key, value)
		}

		n.Content = kept
	case yaml.SequenceNode:
		for _, c := range n.Content {
			pruneZeroNodes(c)
		}
	case yaml.ScalarNode, yaml.AliasNode:
		return
	}
}

func isZeroNode(n *yaml.Node) bool {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return true
		}

		switch n.Value {
		case "", "0", "false", "0s":
			return true
		}

		return false
	case yaml.MappingNode, yaml.SequenceNode:
		return len(n.Content) == 0
	case yaml.DocumentNode, yaml.AliasNode:
		return false
	default:
		return false
	}
}

func printWarnings(cmd *cobra.Command, warnings []openapi.Warning) {
	for _, w := range warnings {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
	}
}

func writeOutput(cmd *cobra.Command, path string, data []byte) error {
	if path == "" || path == "-" {
		_, err := cmd.OutOrStdout().Write(data)
		return err
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "created %s\n", path)

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
