// Package server exposes dbdense context over MCP using stdio transport.
package server

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/valkdb/dbdense/internal/compile"
	"github.com/valkdb/dbdense/pkg/schema"
)

// Serve starts an MCP stdio server that exposes compiled database context
// to LLM clients. It loads a ctxexport.json and provides a lightweight
// table map (lighthouse) as a resource plus an on-demand slice tool for
// full column detail on specific tables.
func Serve(ctx context.Context, exportPath string, version string) error {
	if version == "" {
		return fmt.Errorf("serve: version must not be empty")
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	export, err := schema.LoadExport(exportPath)
	if err != nil {
		return fmt.Errorf("load export: %w", err)
	}

	c := &compile.Compiler{Export: export}

	lighthouseResult, err := c.CompileLighthouse()
	if err != nil {
		return fmt.Errorf("compile lighthouse: %w", err)
	}

	h := &handler{
		export:   export,
		cachedLH: lighthouseResult.DSL,
		sent:     make(map[string]bool, len(export.Entities)),
	}

	s := server.NewMCPServer(
		"dbdense",
		version,
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
	)

	s.AddResource(mcp.NewResource(
		"dbdense://lighthouse",
		"table-map",
		mcp.WithResourceDescription("Lightweight table map (lighthouse.v0) — lists all tables and their FK join targets. Read this first to understand the schema topology, then use the slice tool for column details on specific tables."),
		mcp.WithMIMEType("text/plain"),
	), h.handleLighthouseResource)

	s.AddTool(mcp.NewTool("slice",
		mcp.WithDescription("Get full column-level DDL for specific tables. Read the lighthouse resource first to see available tables, then call this tool with the table names you need."),
		mcp.WithArray("tables",
			mcp.Required(),
			mcp.Description("Array of table names to retrieve DDL for (from lighthouse)"),
			mcp.WithStringItems(),
		),
	), h.handleSliceTool)

	s.AddTool(mcp.NewTool("reset",
		mcp.WithDescription("Clear the session dedup cache so all tables are re-sent on the next slice call. Does NOT reload the export file — restart the server for schema changes."),
	), h.handleResetTool)

	srv := server.NewStdioServer(s)
	return srv.Listen(ctx, os.Stdin, os.Stdout)
}

// handler holds the loaded export and implements MCP resource/tool callbacks.
type handler struct {
	export   *schema.CtxExport
	cachedLH string
	mu       sync.Mutex      // guards sent map against concurrent slice calls
	sent     map[string]bool // tables already delivered this session
}

// handleLighthouseResource returns the lightweight table map.
func (h *handler) handleLighthouseResource(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "dbdense://lighthouse",
			MIMEType: "text/plain",
			Text:     h.cachedLH,
		},
	}, nil
}

// handleSliceTool compiles DDL for the requested tables and returns it.
// Tables already sent in this session are deduplicated — the response
// notes which tables were skipped to avoid redundant context.
//
// Note: there is a benign TOCTOU gap between the first lock (which checks
// the sent map) and the second lock (which marks tables as sent). Two
// concurrent requests for the same tables could both pass the dedup check
// and both return DDL. This is intentional: the worst case is duplicate
// DDL in the LLM context, which wastes tokens but causes no data
// corruption. Holding the lock across the compile step would serialize
// all slice calls, which is worse for interactive latency.
func (h *handler) handleSliceTool(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	if args == nil {
		return mcp.NewToolResultError("missing required parameter: tables"), nil
	}

	rawTables, ok := args["tables"]
	if !ok {
		return mcp.NewToolResultError("missing required parameter: tables"), nil
	}

	tableNames, err := parseStringSlice(rawTables)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid tables parameter: %v", err)), nil
	}

	if len(tableNames) == 0 {
		return mcp.NewToolResultError("tables array must not be empty"), nil
	}

	h.mu.Lock()
	newTables := make([]string, 0, len(tableNames))
	skipped := make([]string, 0, len(tableNames))
	for _, name := range tableNames {
		if h.sent[name] {
			skipped = append(skipped, name)
		} else {
			newTables = append(newTables, name)
		}
	}

	// All requested tables were already sent.
	if len(newTables) == 0 {
		msg := fmt.Sprintf("All requested tables are already in your context: %s",
			strings.Join(skipped, ", "))
		h.mu.Unlock()
		return mcp.NewToolResultText(msg), nil
	}
	h.mu.Unlock()

	c := &compile.Compiler{Export: h.export}
	result, err := c.CompileSubset(newTables)
	if err != nil {
		return nil, fmt.Errorf("compile subset: %w", err)
	}

	// Build a set of tables that actually exist in the compiled result,
	// so we only mark real tables as sent (not nonexistent ones).
	existsSet := make(map[string]bool, len(result.Entities))
	for _, ent := range result.Entities {
		existsSet[ent.Name] = true
	}

	// Identify tables that were requested but don't exist in the export.
	notFound := make([]string, 0, len(newTables))
	toMarkSent := make([]string, 0, len(result.Entities))
	for _, name := range newTables {
		if existsSet[name] {
			toMarkSent = append(toMarkSent, name)
		} else {
			notFound = append(notFound, name)
		}
	}

	h.mu.Lock()
	for _, name := range toMarkSent {
		h.sent[name] = true
	}
	h.mu.Unlock()

	// Build response text: DDL plus optional notes.
	var out strings.Builder
	out.WriteString(result.DSL)
	if len(notFound) > 0 {
		fmt.Fprintf(&out, "\n-- Warning: tables not found in schema: %s\n",
			strings.Join(notFound, ", "))
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&out, "\n-- Note: skipped (already in context): %s\n",
			strings.Join(skipped, ", "))
	}

	return mcp.NewToolResultText(out.String()), nil
}

// handleResetTool clears the session deduplication cache so that all tables
// will be re-sent on the next slice call.
func (h *handler) handleResetTool(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.mu.Lock()
	h.sent = make(map[string]bool, len(h.export.Entities))
	h.mu.Unlock()
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{Type: "text", Text: "Session cache cleared. All tables will be re-sent on next slice call."},
		},
	}, nil
}

// parseStringSlice converts a JSON-decoded value (expected to be []any with
// string elements) into a []string. Returns an error if the type is wrong.
func parseStringSlice(v any) ([]string, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array, got %T", v)
	}

	result := make([]string, 0, len(arr))
	for i, elem := range arr {
		s, ok := elem.(string)
		if !ok {
			return nil, fmt.Errorf("element %d: expected string, got %T", i, elem)
		}
		result = append(result, s)
	}
	return result, nil
}
