// Package main provides the dbdense CLI entrypoint.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/valkdb/dbdense/internal/compile"
	"github.com/valkdb/dbdense/internal/extract"
	"github.com/valkdb/dbdense/internal/gen"
	"github.com/valkdb/dbdense/internal/server"
	"github.com/valkdb/dbdense/pkg/schema"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:           "dbdense",
		Short:         "A Database Context Compiler for LLM Agents",
		Long:          "dbdense extracts, compiles, and serves optimized database schema context to LLMs via the Model Context Protocol.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       version,
	}

	root.AddCommand(
		newExportCmd(),
		newCompileCmd(),

		newServeCmd(),
		newGenExportCmd(),
		newInitClaudeCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newExportCmd builds the "export" subcommand that extracts DB metadata.
func newExportCmd() *cobra.Command {
	var (
		dsn        string
		driver     string
		schemas    string
		out        string
		sidecar    string
		sampleSize int
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Extract database metadata into a canonical ctxexport.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			ext, err := buildExtractor(driver, dsn, schemas, sampleSize)
			if err != nil {
				return err
			}

			export, err := ext.Extract(ctx)
			if err != nil {
				return fmt.Errorf("extraction failed: %w", err)
			}

			if len(export.Entities) == 0 {
				fmt.Fprintln(os.Stderr, "WARNING: no entities found. Check your connection string and --schemas flag.")
			}

			// Surface extractor warnings (e.g., cross-schema FK targets,
			// conservative MongoDB inferred-ref skips, small collections).
			for _, w := range ext.Warnings() {
				fmt.Fprintln(os.Stderr, "WARNING:", w)
			}

			sc, err := extract.LoadSidecar(sidecar)
			if err != nil {
				return fmt.Errorf("sidecar load failed: %w", err)
			}
			warnings := extract.MergeSidecar(export, sc)
			for _, w := range warnings {
				fmt.Fprintln(os.Stderr, "WARNING:", w)
			}

			f, err := os.Create(out)
			if err != nil {
				return fmt.Errorf("create output: %w", err)
			}

			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			if err := enc.Encode(export); err != nil {
				_ = f.Close()
				return fmt.Errorf("write json: %w", err)
			}

			if err := f.Close(); err != nil {
				return fmt.Errorf("close output: %w", err)
			}

			fmt.Fprintf(os.Stderr, "exported %d entities, %d edges -> %s\n",
				len(export.Entities), len(export.Edges), out)
			return nil
		},
	}

	cmd.Flags().StringVar(&dsn, "db", "", "database connection string (e.g. postgres://...)")
	cmd.Flags().StringVar(&driver, "driver", "postgres", "database driver: postgres, mongodb")
	cmd.Flags().StringVar(&schemas, "schemas", "", "comma-separated schemas/databases (default: driver-specific)")
	cmd.Flags().StringVar(&out, "out", "ctxexport.json", "output file path for the canonical export")
	cmd.Flags().StringVar(&sidecar, "sidecar", "dbdense.yaml", "path to the sidecar override file")
	cmd.Flags().IntVar(&sampleSize, "sample-size", extract.DefaultSampleSize, "number of documents to sample per MongoDB collection")
	_ = cmd.MarkFlagRequired("db")

	return cmd
}

// buildExtractor returns the appropriate Extractor for the given driver.
func buildExtractor(driver, dsn, schemas string, sampleSize int) (extract.Extractor, error) {
	schemaList := splitSchemas(schemas)

	ext, ok := extract.New(driver)
	if !ok {
		return nil, fmt.Errorf("unsupported driver %q; available: %s", driver, strings.Join(extract.Available(), ", "))
	}

	// Configure common fields via the Configurable interface.
	if c, ok := ext.(extract.Configurable); ok {
		c.SetDSN(dsn)
		if len(schemaList) > 0 {
			c.SetSchemas(schemaList)
		}
	}

	// MongoDB-specific: default database and sample size.
	if m, ok := ext.(*extract.MongoExtractor); ok {
		m.SampleSize = sampleSize
		if m.Database == "" {
			m.Database = "test"
			fmt.Fprintln(os.Stderr, "WARNING: no --schemas specified; defaulting to database 'test'. Use --schemas to select your database.")
		}
	}

	return ext, nil
}

// splitSchemas splits a comma-separated schema list, trimming whitespace.
func splitSchemas(s string) []string {
	if s == "" {
		return nil
	}
	result := make([]string, 0, strings.Count(s, ",")+1)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// newCompileCmd builds the "compile" subcommand that renders the full schema
// into ctxpack.v0 without question-based slicing.
func newCompileCmd() *cobra.Command {
	var (
		in      string
		out     string
		outDir  string
		splitBy string
		mode    string
	)

	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile full ctxpack.v0 context from ctxexport.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			export, err := schema.LoadExport(in)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("%w; run 'dbdense export' first to create one, or 'dbdense genexport' for a demo", err)
				}
				return err
			}

			if normalizeMode(mode) == "lighthouse" {
				return compileLighthouse(export, out)
			}

			switch normalizeSplitBy(splitBy) {
			case "":
				// Default behavior: one full ctxpack file.
			case "schema":
				return compileSplitBySchema(export, outDir)
			default:
				return fmt.Errorf("unsupported --split-by %q (supported: schema)", splitBy)
			}

			c := &compile.Compiler{Export: export}
			result, err := c.CompileAll()
			if err != nil {
				return err
			}

			f, err := os.Create(out)
			if err != nil {
				return fmt.Errorf("create output: %w", err)
			}

			if err := compile.WriteDDL(f, result.Entities, result.Edges); err != nil {
				_ = f.Close()
				return fmt.Errorf("write ddl: %w", err)
			}

			if err := f.Close(); err != nil {
				return fmt.Errorf("close output: %w", err)
			}

			fmt.Fprintf(os.Stderr, "compiled %d entities, %d edges -> %s\n",
				len(result.Entities), len(result.Edges), out)
			return nil
		},
	}

	cmd.Flags().StringVar(&in, "in", "ctxexport.json", "path to the canonical ctxexport.json")
	cmd.Flags().StringVar(&out, "out", "ctxpack.txt", "output file path for full compiled context")
	cmd.Flags().StringVar(&splitBy, "split-by", "", "optional split mode for compile output (supported: schema)")
	cmd.Flags().StringVar(&outDir, "out-dir", "ctxpacks", "output directory used when --split-by is set")
	cmd.Flags().StringVar(&mode, "mode", "", "compile mode: lighthouse (lightweight table map) or empty for full ctxpack")

	return cmd
}

// newServeCmd builds the "serve" subcommand that starts an MCP stdio server.
func newServeCmd() *cobra.Command {
	var in string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start an MCP stdio server exposing compiled context to LLM clients",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "starting MCP stdio server")
			err := server.Serve(cmd.Context(), in, version)
			if err != nil && errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w; run 'dbdense export' first to create one, or 'dbdense genexport' for a demo", err)
			}
			return err
		},
	}

	cmd.Flags().StringVar(&in, "in", "ctxexport.json", "path to the canonical ctxexport.json")

	return cmd
}

// newGenExportCmd builds the "genexport" subcommand that generates synthetic
// ctxexport.json files for stress testing.
func newGenExportCmd() *cobra.Command {
	var (
		tables int
		out    string
		seed   int64
	)

	cmd := &cobra.Command{
		Use:   "genexport",
		Short: "Generate a synthetic ctxexport.json for stress testing",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := gen.Config{
				TotalTables:  tables,
				SignalTables: gen.DefaultSignalTables(),
				Seed:         seed,
			}

			export := gen.Generate(cfg)

			f, err := os.Create(out)
			if err != nil {
				return fmt.Errorf("create output: %w", err)
			}

			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			if err := enc.Encode(export); err != nil {
				_ = f.Close()
				return fmt.Errorf("write json: %w", err)
			}

			if err := f.Close(); err != nil {
				return fmt.Errorf("close output: %w", err)
			}

			fmt.Fprintf(os.Stderr, "generated %d entities, %d edges -> %s\n",
				len(export.Entities), len(export.Edges), out)
			return nil
		},
	}

	cmd.Flags().IntVar(&tables, "tables", 100, "total number of tables to generate")
	cmd.Flags().StringVar(&out, "out", "ctxexport.json", "output file path")
	cmd.Flags().Int64Var(&seed, "seed", 0, "random seed (0 = use table count)")

	return cmd
}

func normalizeMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "lighthouse", "lh":
		return "lighthouse"
	default:
		return ""
	}
}

func compileLighthouse(export *schema.CtxExport, out string) error {
	if out == "ctxpack.txt" {
		out = "lighthouse.txt"
	}

	c := &compile.Compiler{Export: export}
	result, err := c.CompileLighthouse()
	if err != nil {
		return err
	}

	f, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}

	if _, err := fmt.Fprint(f, result.DSL); err != nil {
		_ = f.Close()
		return fmt.Errorf("write lighthouse: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}

	// charsPerToken is a rough approximation of characters per LLM token,
	// used only for the informational log line.
	const charsPerToken = 4
	tokens := len(result.DSL) / charsPerToken
	fmt.Fprintf(os.Stderr, "lighthouse: %d tables, ~%d tokens -> %s\n",
		len(result.Entities), tokens, out)
	return nil
}

func normalizeSplitBy(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none":
		return ""
	case "schema", "namespace":
		return "schema"
	default:
		return strings.ToLower(strings.TrimSpace(v))
	}
}

func compileSplitBySchema(export *schema.CtxExport, outDir string) error {
	if strings.TrimSpace(outDir) == "" {
		return fmt.Errorf("--out-dir must not be empty when --split-by is set")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	type nsBundle struct {
		entities            []schema.Entity
		edges               []schema.Edge
		hasCrossSchemaEdges bool
	}

	entityToNS := make(map[string]string, len(export.Entities))
	// initialBundleEstimate is a rough guess for distinct namespaces; most
	// schemas have 1-4 namespaces (public + a few application schemas).
	const initialBundleEstimate = 4
	bundles := make(map[string]*nsBundle, initialBundleEstimate)

	for _, ent := range export.Entities {
		ns := entityNamespace(ent.Name)
		entityToNS[ent.Name] = ns
		if bundles[ns] == nil {
			bundles[ns] = &nsBundle{}
		}
		bundles[ns].entities = append(bundles[ns].entities, ent)
	}

	for _, edge := range export.Edges {
		fromNS := entityToNS[edge.FromEntity]
		toNS := entityToNS[edge.ToEntity]
		if fromNS == "" && toNS == "" {
			continue
		}

		crossSchema := fromNS != "" && toNS != "" && fromNS != toNS

		if fromNS != "" {
			bundles[fromNS].edges = append(bundles[fromNS].edges, edge)
			if crossSchema {
				bundles[fromNS].hasCrossSchemaEdges = true
			}
		}
		if toNS != "" && toNS != fromNS {
			bundles[toNS].edges = append(bundles[toNS].edges, edge)
			if crossSchema {
				bundles[toNS].hasCrossSchemaEdges = true
			}
		}
	}

	namespaces := make([]string, 0, len(bundles))
	for ns := range bundles {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	written := 0
	for _, ns := range namespaces {
		b := bundles[ns]
		if len(b.entities) == 0 {
			continue
		}

		path := filepath.Join(outDir, namespaceFileName(ns))
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create output: %w", err)
		}
		if b.hasCrossSchemaEdges {
			if _, err := fmt.Fprintln(f, "-- Note: includes cross-schema foreign keys touching entities outside this schema bundle."); err != nil {
				_ = f.Close()
				return fmt.Errorf("write comment: %w", err)
			}
		}
		if err := compile.WriteDDL(f, b.entities, b.edges); err != nil {
			_ = f.Close()
			return fmt.Errorf("write ddl: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close output: %w", err)
		}
		written++
	}

	fmt.Fprintf(os.Stderr, "compiled split ctxpacks by schema: %d files -> %s\n", written, outDir)
	return nil
}

func entityNamespace(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "default"
	}
	if dot := strings.Index(trimmed, "."); dot > 0 {
		return trimmed[:dot]
	}
	return "default"
}

func namespaceFileName(ns string) string {
	ns = strings.TrimSpace(strings.ToLower(ns))
	if ns == "" {
		ns = "default"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range ns {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "default"
	}
	return out + ".ctxpack.txt"
}

// newInitClaudeCmd builds the "init-claude" subcommand that writes
// MCP configuration for Claude Code (.mcp.json) or Claude Desktop
// (claude_desktop_config.json).
func newInitClaudeCmd() *cobra.Command {
	var (
		in     string
		target string
	)

	cmd := &cobra.Command{
		Use:   "init-claude",
		Short: "Generate MCP config for Claude Code or Claude Desktop",
		Long: `Writes the MCP server configuration so Claude can discover dbdense automatically.

For Claude Code (default), creates or updates .mcp.json in the current directory.
For Claude Desktop, creates or updates claude_desktop_config.json.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInitClaude(in, target)
		},
	}

	cmd.Flags().StringVar(&in, "in", "ctxexport.json", "path to the ctxexport.json (used in the generated config)")
	cmd.Flags().StringVar(&target, "target", "claude-code", "config target: claude-code or claude-desktop")

	return cmd
}

// mcpConfig represents the .mcp.json / claude_desktop_config.json structure.
type mcpConfig struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func runInitClaude(exportPath, target string) error {
	// Resolve the dbdense binary path for the config.
	binPath, err := os.Executable()
	if err != nil {
		binPath = "dbdense" // fallback to PATH lookup
	}

	exportPath, err = filepath.Abs(exportPath)
	if err != nil {
		return fmt.Errorf("resolve export path: %w", err)
	}

	entry := mcpServerEntry{
		Command: binPath,
		Args:    []string{"serve", "--in", exportPath},
	}

	var configPath string
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "claude-code", "code", "":
		configPath = ".mcp.json"
	case "claude-desktop", "desktop":
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		configPath = filepath.Join(home, ".claude", "claude_desktop_config.json")
	default:
		return fmt.Errorf("unsupported target %q (supported: claude-code, claude-desktop)", target)
	}

	// Load existing config if present, to preserve other MCP servers.
	var cfg mcpConfig
	existing, err := os.ReadFile(configPath)
	if err == nil {
		// File exists — parse it.
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return fmt.Errorf("existing %s is not valid JSON: %w", configPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read config %s: %w", configPath, err)
	}

	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]mcpServerEntry, 1) // typically just the dbdense entry
	}

	if _, exists := cfg.MCPServers["dbdense"]; exists {
		fmt.Fprintf(os.Stderr, "updating existing dbdense entry in %s\n", configPath)
	} else {
		fmt.Fprintf(os.Stderr, "adding dbdense to %s\n", configPath)
	}
	cfg.MCPServers["dbdense"] = entry

	// Ensure parent directory exists (for claude-desktop path).
	dir := filepath.Dir(configPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "done. Restart Claude to pick up the new MCP server.\n")
	return nil
}
