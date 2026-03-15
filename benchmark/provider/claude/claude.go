// Package claude implements a direct Claude CLI benchmark provider.
package claude

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/valkdb/dbdense/benchmark/provider"
)

const (
	defaultCLICommand = "claude"
	providerID        = "claude"

	metaMCPConfig       = "claude_mcp_config"
	metaStrictMCPConfig = "claude_strict_mcp_config"
	metaPermissionMode  = "claude_permission_mode"

	// charsPerToken is used only for the legacy estimated_input_tokens field.
	// For SQL DDL, the actual ratio is closer to 3.0-3.5 chars/token, so this
	// heuristic underestimates by ~15-25%. The real benchmark uses provider-
	// reported token counts (prompt_tokens), not this estimate.
	charsPerToken = 4
)

type claudeProvider struct {
	command string
	args    []string
}

type claudeSession struct {
	provider     *claudeProvider
	localID      string
	remoteID     string
	model        string
	systemPrompt string
}

type cliResponse struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	IsError    bool            `json:"is_error"`
	Result     string          `json:"result"`
	SessionID  string          `json:"session_id"`
	DurationMs int64           `json:"duration_ms"`
	NumTurns   int             `json:"num_turns"`
	Usage      cliUsage        `json:"usage"`
	Raw        json.RawMessage `json:"-"`

	// Fallback: some CLI versions report tokens at top level.
	TopInputTokens  int `json:"input_tokens"`
	TopOutputTokens int `json:"output_tokens"`
}

type cliUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

// totalInputTokens returns the real total context tokens the model processed,
// including cached portions that the CLI reports separately.
func (u cliUsage) totalInputTokens() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// New returns a Claude provider that shells out to the local Claude CLI.
//
// Environment:
// - BENCH_CLAUDE_CLI      (default: claude)
// - BENCH_CLAUDE_CLI_ARGS (space-separated, optional)
func New() (provider.Provider, error) {
	command := strings.TrimSpace(os.Getenv("BENCH_CLAUDE_CLI"))
	if command == "" {
		command = defaultCLICommand
	}
	if _, err := exec.LookPath(command); err != nil {
		return nil, fmtProviderUnavailable(command, err)
	}

	return &claudeProvider{
		command: command,
		args:    strings.Fields(os.Getenv("BENCH_CLAUDE_CLI_ARGS")),
	}, nil
}

func (p *claudeProvider) ID() string {
	return providerID
}

func (p *claudeProvider) NewSession(_ context.Context, cfg provider.SessionConfig) (provider.Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	sid, err := newSessionID()
	if err != nil {
		return nil, err
	}

	return &claudeSession{
		provider:     p,
		localID:      sid,
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,
	}, nil
}

func (s *claudeSession) ID() string {
	if strings.TrimSpace(s.remoteID) != "" {
		return strings.TrimSpace(s.remoteID)
	}
	return s.localID
}

func (s *claudeSession) ProviderID() string {
	return providerID
}

func (s *claudeSession) ModelID() string {
	return s.model
}

func (s *claudeSession) Query(ctx context.Context, req provider.QueryRequest) (*provider.QueryResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	prompt := buildPrompt(req)
	promptChars := len(prompt)
	estimatedInputTokens := promptChars / charsPerToken
	args := make([]string, 0, 16+len(s.provider.args))
	args = append(args, "-p", "--output-format", "json", "--model", s.model)
	if strings.TrimSpace(s.remoteID) != "" {
		args = append(args, "--resume", strings.TrimSpace(s.remoteID))
	}
	if mode := strings.TrimSpace(req.Metadata[metaPermissionMode]); mode != "" {
		args = append(args, "--permission-mode", mode)
	}
	if strictMCPEnabled(req.Metadata) {
		args = append(args, "--strict-mcp-config")
	}
	args = append(args, mcpConfigArgs(req.Metadata)...)
	if strings.TrimSpace(s.systemPrompt) != "" {
		args = append(args, "--system-prompt", s.systemPrompt)
	}
	args = append(args, s.provider.args...)

	cmd := exec.CommandContext(ctx, s.provider.command, args...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmtProviderQueryTimeout(err, stdout.String(), stderr.String())
		}
		return nil, fmtProviderQueryFailed(err, stdout.String(), stderr.String())
	}

	out := strings.TrimSpace(stdout.String())
	var resp cliResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmtProviderProtocol("invalid Claude CLI JSON", err, out)
	}
	resp.Raw = json.RawMessage(out)

	if resp.IsError {
		return nil, fmtProviderProtocol("Claude CLI returned is_error=true", nil, out)
	}
	if strings.TrimSpace(resp.Result) == "" {
		return nil, fmtProviderProtocol("Claude CLI result is empty", nil, out)
	}
	if strings.TrimSpace(resp.SessionID) != "" {
		s.remoteID = strings.TrimSpace(resp.SessionID)
	}

	// Use real total input tokens (includes cache creation + cache read).
	inputTokens := resp.Usage.totalInputTokens()
	outputTokens := resp.Usage.OutputTokens
	// Fallback: some CLI versions report tokens at top level, not under usage.
	if inputTokens == 0 && resp.TopInputTokens > 0 {
		inputTokens = resp.TopInputTokens
	}
	if outputTokens == 0 && resp.TopOutputTokens > 0 {
		outputTokens = resp.TopOutputTokens
	}
	usage := provider.UsageMetrics{
		PromptTokens:     inputTokens,
		CompletionTokens: outputTokens,
		TotalTokens:      inputTokens + outputTokens,
	}

	return &provider.QueryResult{
		Answer: resp.Result,
		Usage:  usage,
		Timing: provider.TimingMetrics{
			LatencyMs: resp.DurationMs,
		},
		Raw:                  resp.Raw,
		PromptChars:          promptChars,
		EstimatedInputTokens: estimatedInputTokens,
		NumTurns:             resp.NumTurns,
	}, nil
}

func (s *claudeSession) Close(_ context.Context) error {
	return nil
}

func buildPrompt(req provider.QueryRequest) string {
	if strings.TrimSpace(req.MCPContext) == "" {
		return req.Prompt
	}

	var b strings.Builder
	b.WriteString("Use this precompiled database context when answering.\n")
	b.WriteString("If context conflicts with assumptions, trust the context.\n\n")
	b.WriteString("[SCHEMA_CONTEXT_START]\n")
	b.WriteString(req.MCPContext)
	b.WriteString("\n[SCHEMA_CONTEXT_END]\n\n")
	b.WriteString("Task:\n")
	b.WriteString(req.Prompt)
	return b.String()
}

func mcpConfigArgs(meta map[string]string) []string {
	raw := strings.TrimSpace(meta[metaMCPConfig])
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	args := make([]string, 0, len(parts)*2)
	for _, part := range parts {
		cfg := strings.TrimSpace(part)
		if cfg == "" {
			continue
		}
		args = append(args, "--mcp-config", cfg)
	}
	return args
}

func strictMCPEnabled(meta map[string]string) bool {
	switch strings.ToLower(strings.TrimSpace(meta[metaStrictMCPConfig])) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmtProviderProtocol("generate session id", err, "")
	}

	// RFC 4122 variant + version 4 bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]),
	), nil
}

func fmtProviderUnavailable(command string, err error) error {
	return fmt.Errorf("%w: provider %q command %q not found: %v", provider.ErrProviderUnavailable, providerID, command, err)
}

func fmtProviderQueryFailed(runErr error, stdout, stderr string) error {
	return fmt.Errorf("%w: provider %q CLI call failed: %v stdout=%q stderr=%q", provider.ErrProviderExecution, providerID, runErr, stdout, stderr)
}

func fmtProviderQueryTimeout(runErr error, stdout, stderr string) error {
	return fmt.Errorf("%w: provider %q CLI call timed out (increase --query-timeout): %v stdout=%q stderr=%q", provider.ErrProviderExecution, providerID, runErr, stdout, stderr)
}

func fmtProviderProtocol(msg string, cause error, raw string) error {
	if cause != nil {
		return fmt.Errorf("%w: provider %q %s: %v raw=%q", provider.ErrProviderProtocol, providerID, msg, cause, raw)
	}
	return fmt.Errorf("%w: provider %q %s raw=%q", provider.ErrProviderProtocol, providerID, msg, raw)
}
