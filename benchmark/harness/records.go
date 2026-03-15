package harness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/valkdb/dbdense/benchmark/provider"
)

// PricingSnapshot captures token pricing used to calculate per-run API cost.
type PricingSnapshot struct {
	ProviderID     string    `json:"provider_id"`
	ModelID        string    `json:"model_id"`
	InputPer1KUSD  float64   `json:"input_per_1k_usd"`
	OutputPer1KUSD float64   `json:"output_per_1k_usd"`
	CapturedAt     time.Time `json:"captured_at"`
	Source         string    `json:"source"`
}

// AccuracyMetrics captures deterministic scenario scoring outputs.
type AccuracyMetrics struct {
	Score          float64  `json:"score"`
	Pass           bool     `json:"pass"`
	Matched        []string `json:"matched,omitempty"`
	Missing        []string `json:"missing,omitempty"`
	ForbiddenFound []string `json:"forbidden_found,omitempty"`
}

// RunRecord is the raw unit of evidence for one scenario/arm/repetition.
type RunRecord struct {
	RunID            string `json:"run_id"`
	ScenarioID       string `json:"scenario_id"`
	ScenarioLabel    string `json:"scenario_label"`
	ScenarioCategory string `json:"scenario_category"`
	Arm              string `json:"arm"`
	ProviderID       string `json:"provider_id"`
	ModelID          string `json:"model_id"`
	SessionID        string `json:"session_id,omitempty"`
	Iteration        int    `json:"iteration"`
	Prompt           string `json:"prompt"`
	MCPContextBytes  int    `json:"mcp_context_bytes,omitempty"`

	// PromptChars is the total character count of the full prompt (including
	// context payload and boilerplate wrapping) sent to the provider.
	PromptChars int `json:"prompt_chars,omitempty"`

	// EstimatedInputTokens is a local estimate of input tokens computed as
	// PromptChars / charsPerToken, used when provider-reported prompt_tokens
	// is unreliable (e.g. Claude CLI reports ~2-3 regardless of actual input).
	EstimatedInputTokens int `json:"estimated_input_tokens,omitempty"`

	StartedAt     time.Time              `json:"started_at"`
	FinishedAt    time.Time              `json:"finished_at"`
	WallLatencyMs int64                  `json:"wall_latency_ms"`
	Answer        string                 `json:"answer,omitempty"`
	Usage         provider.UsageMetrics  `json:"usage,omitempty"`
	Timing        provider.TimingMetrics `json:"timing,omitempty"`
	ToolCalls     []provider.ToolCall    `json:"tool_calls,omitempty"`

	// NumTurns is the number of agentic turns (tool-call round-trips).
	// More turns = more total context tokens processed by the model.
	NumTurns int `json:"num_turns,omitempty"`

	Accuracy          AccuracyMetrics `json:"accuracy"`
	Complete          bool            `json:"complete"`
	IncompleteReasons []string        `json:"incomplete_reasons,omitempty"`
	Error             string          `json:"error,omitempty"`
}

// ResultManifest summarizes one benchmark run output directory.
type ResultManifest struct {
	RunStamp    string          `json:"run_stamp"`
	CreatedAt   time.Time       `json:"created_at"`
	ProviderID  string          `json:"provider_id"`
	ModelID     string          `json:"model_id"`
	Arms        []string        `json:"arms"`
	ScenarioIDs []string        `json:"scenario_ids"`
	TotalRuns   int             `json:"total_runs"`
	Pricing     PricingSnapshot `json:"pricing"`
}

// EnsureOutputDir creates results/<stamp> and returns the absolute path.
func EnsureOutputDir(resultsRoot string) (runStamp string, absDir string, err error) {
	runStamp = time.Now().UTC().Format("20060102T150405Z")
	dir := filepath.Join(resultsRoot, runStamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create output dir: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", fmt.Errorf("resolve output dir: %w", err)
	}
	return runStamp, abs, nil
}

// WriteJSONFile writes v as indented JSON.
func WriteJSONFile(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = f.Close()
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// AppendJSONL appends one JSON object line.
func AppendJSONL(path string, v any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	enc := json.NewEncoder(f)
	if err := enc.Encode(v); err != nil {
		_ = f.Close()
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// LoadRunRecords reads runs.jsonl into memory.
func LoadRunRecords(path string) ([]RunRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var out []RunRecord
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var r RunRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		out = append(out, r)
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// UniqueScenarioIDs returns sorted scenario IDs from records.
func UniqueScenarioIDs(records []RunRecord) []string {
	set := make(map[string]bool, len(records))
	for _, r := range records {
		set[r.ScenarioID] = true
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// UniqueArms returns sorted arm names from records.
func UniqueArms(records []RunRecord) []string {
	set := make(map[string]bool, len(records))
	for _, r := range records {
		set[r.Arm] = true
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
