// benchcheck validates provider wiring by starting a real provider
// session and executing one prompt.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/valkdb/dbdense/benchmark/provider"
	"github.com/valkdb/dbdense/benchmark/provider/registry"
)

func main() {
	var (
		providerID          string
		modelID             string
		prompt              string
		runID               string
		scenarioID          string
		mcpContext          string
		mcpContextFile      string
		mcpConfigCSV        string
		permissionMode      string
		timeout             time.Duration
		strictMCPConfigFlag bool
	)

	flag.StringVar(&providerID, "provider", "claude", "provider id (claude)")
	flag.StringVar(&modelID, "model", "", "model id (required)")
	flag.StringVar(&prompt, "prompt", "Return a short ack.", "prompt to execute")
	flag.StringVar(&runID, "run-id", "manual-check", "benchmark run id")
	flag.StringVar(&scenarioID, "scenario-id", "manual", "scenario id")
	flag.StringVar(&mcpContext, "mcp-context", "", "optional mcp context payload")
	flag.StringVar(&mcpContextFile, "mcp-context-file", "", "optional text file loaded into mcp context payload")
	flag.StringVar(&mcpConfigCSV, "mcp-config", "", "comma-separated MCP config paths/json for Claude CLI")
	flag.BoolVar(&strictMCPConfigFlag, "strict-mcp-config", false, "pass --strict-mcp-config to Claude CLI")
	flag.StringVar(&permissionMode, "claude-permission-mode", "", "optional Claude permission mode")
	flag.DurationVar(&timeout, "timeout", 2*time.Minute, "overall timeout")
	flag.Parse()

	if modelID == "" {
		exitErr("-model is required")
	}
	if strings.TrimSpace(mcpContext) != "" && strings.TrimSpace(mcpContextFile) != "" {
		exitErr("use only one of -mcp-context or -mcp-context-file")
	}
	if strings.TrimSpace(mcpContextFile) != "" {
		loaded, err := os.ReadFile(strings.TrimSpace(mcpContextFile))
		if err != nil {
			exitErr(fmt.Sprintf("read mcp context file: %v", err))
		}
		mcpContext = string(loaded)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	prov, err := registry.New(providerID)
	if err != nil {
		exitErr(err.Error())
	}

	sess, err := prov.NewSession(ctx, provider.SessionConfig{Model: modelID})
	if err != nil {
		exitErr(err.Error())
	}
	defer func() {
		_ = sess.Close(context.Background())
	}()

	// maxMetadataKeys is the number of possible metadata keys we set below.
	const maxMetadataKeys = 3
	metadata := make(map[string]string, maxMetadataKeys)
	if mcp := mcpConfigCSV; mcp != "" {
		metadata["claude_mcp_config"] = mcp
	}
	if strictMCPConfigFlag {
		metadata["claude_strict_mcp_config"] = "true"
	}
	if permissionMode != "" {
		metadata["claude_permission_mode"] = permissionMode
	}

	result, err := sess.Query(ctx, provider.QueryRequest{
		RunID:      runID,
		ScenarioID: scenarioID,
		Prompt:     prompt,
		MCPContext: mcpContext,
		Metadata:   metadata,
	})
	if err != nil {
		exitErr(err.Error())
	}

	// outputFieldCount is the number of fields in the JSON output object.
	const outputFieldCount = 4
	output := make(map[string]any, outputFieldCount)
	output["provider_id"] = prov.ID()
	output["model_id"] = sess.ModelID()
	output["session_id"] = sess.ID()
	output["result"] = result

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		exitErr(fmt.Sprintf("encode output: %v", err))
	}
}

func exitErr(msg string) {
	fmt.Fprintln(os.Stderr, "benchcheck error:", msg)
	os.Exit(1)
}
