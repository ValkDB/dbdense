package claude

import (
	"strings"
	"testing"

	"github.com/valkdb/dbdense/benchmark/provider"
)

func TestBuildPromptWithContext(t *testing.T) {
	req := provider.QueryRequest{
		Prompt:     "SELECT 1",
		MCPContext: "-- dbdense schema context\nCREATE TABLE users (\n  id uuid PRIMARY KEY\n);\n",
	}
	prompt := buildPrompt(req)
	if !strings.Contains(prompt, req.MCPContext) {
		t.Error("prompt should contain the MCP context verbatim")
	}
	if !strings.Contains(prompt, req.Prompt) {
		t.Error("prompt should contain the original prompt")
	}
	if !strings.Contains(prompt, "[SCHEMA_CONTEXT_START]") {
		t.Error("prompt should contain SCHEMA_CONTEXT_START marker")
	}
	if !strings.Contains(prompt, "[SCHEMA_CONTEXT_END]") {
		t.Error("prompt should contain SCHEMA_CONTEXT_END marker")
	}
}

func TestBuildPromptWithoutContext(t *testing.T) {
	req := provider.QueryRequest{
		Prompt: "SELECT 1",
	}
	prompt := buildPrompt(req)
	if prompt != req.Prompt {
		t.Fatalf("expected raw prompt %q, got %q", req.Prompt, prompt)
	}
}
