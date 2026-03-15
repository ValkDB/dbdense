package provider

import (
	"context"
	"encoding/json"
	"fmt"
)

// Provider creates fresh benchmark sessions for one LLM provider.
type Provider interface {
	ID() string
	NewSession(ctx context.Context, cfg SessionConfig) (Session, error)
}

// Session represents one provider session used by one benchmark run.
type Session interface {
	ID() string
	ProviderID() string
	ModelID() string
	Query(ctx context.Context, req QueryRequest) (*QueryResult, error)
	Close(ctx context.Context) error
}

// SessionConfig configures a fresh provider session.
type SessionConfig struct {
	Model        string            `json:"model"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// QueryRequest is the benchmark input for one provider call.
type QueryRequest struct {
	RunID      string            `json:"run_id,omitempty"`
	ScenarioID string            `json:"scenario_id,omitempty"`
	Prompt     string            `json:"prompt"`
	MCPContext string            `json:"mcp_context,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// UsageMetrics captures provider token usage.
type UsageMetrics struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	MCPPayloadTokens int `json:"mcp_payload_tokens,omitempty"`
}

// TimingMetrics captures provider timing in milliseconds.
type TimingMetrics struct {
	FirstTokenMs int64 `json:"first_token_ms,omitempty"`
	LatencyMs    int64 `json:"latency_ms,omitempty"`
}

// ToolCall captures one MCP/tool call made by the provider.
type ToolCall struct {
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`
}

// QueryResult is one provider answer plus measurable metadata.
type QueryResult struct {
	Answer    string          `json:"answer"`
	Usage     UsageMetrics    `json:"usage,omitempty"`
	Timing    TimingMetrics   `json:"timing,omitempty"`
	ToolCalls []ToolCall      `json:"tool_calls,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`

	// PromptChars is the total character count of the assembled prompt sent to
	// the provider, including any injected context and boilerplate wrapping.
	PromptChars int `json:"prompt_chars,omitempty"`

	// EstimatedInputTokens is a local estimate of input tokens based on
	// prompt character length, used when the provider doesn't report accurate
	// input token counts (e.g. Claude CLI).
	EstimatedInputTokens int `json:"estimated_input_tokens,omitempty"`

	// NumTurns is the number of agentic turns (tool-call round-trips) the
	// provider used to answer the query. Each turn re-sends conversation
	// history, so more turns = more total context tokens processed.
	NumTurns int `json:"num_turns,omitempty"`
}

// Validate ensures session config is usable and explicit.
func (c SessionConfig) Validate() error {
	if c.Model == "" {
		return fmt.Errorf("%w: model is required", ErrInvalidSessionConfig)
	}
	return nil
}

// Validate ensures query request is usable and explicit.
func (r QueryRequest) Validate() error {
	if r.Prompt == "" {
		return fmt.Errorf("%w: prompt is required", ErrInvalidQueryRequest)
	}
	return nil
}
