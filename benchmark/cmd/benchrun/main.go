// benchrun executes benchmark scenarios against a real provider backend and
// persists per-run evidence records under benchmark/results/<run_stamp>/.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/valkdb/dbdense/benchmark/harness"
	"github.com/valkdb/dbdense/benchmark/provider"
	"github.com/valkdb/dbdense/benchmark/provider/registry"
	"github.com/valkdb/dbdense/benchmark/scenario"
)

const (
	armBaseline        = "baseline"
	armDBDense         = "dbdense"
	armDBDenseWithDesc = "dbdense_with_desc"
	armDBDenseLH       = "dbdense_lighthouse"
	armWarmupOnly      = "warmup-only"  // legacy alias -> dbdense
	armWarmSlice       = "warmup+slice" // legacy alias -> dbdense
)

func main() {
	cfg, err := parseFlags()
	if err != nil {
		exitErr(err.Error())
	}

	scenarios, err := scenario.LoadDir(cfg.scenariosDir)
	if err != nil {
		exitErr(err.Error())
	}
	regularScenarios, stressScenarios := splitScenarioCategories(scenarios)

	arms, err := parseArms(cfg.armsCSV)
	if err != nil {
		exitErr(err.Error())
	}
	if err := validateArmInputs(cfg, arms); err != nil {
		exitErr(err.Error())
	}

	prov, err := registry.New(cfg.providerID)
	if err != nil {
		exitErr(err.Error())
	}

	runStamp, outDir, err := harness.EnsureOutputDir(cfg.resultsRoot)
	if err != nil {
		exitErr(err.Error())
	}
	runsPath := filepath.Join(outDir, "runs.jsonl")

	sessionCfg := provider.SessionConfig{
		Model:        cfg.modelID,
		SystemPrompt: cfg.systemPrompt,
		Metadata: map[string]string{
			"harness": "benchrun",
		},
	}

	ctx := context.Background()
	total := 0
	for iter := 1; iter <= cfg.runs; iter++ {
		for _, arm := range arms {
			if len(regularScenarios) > 0 {
				n, err := runScenarioFlow(
					ctx,
					cfg,
					prov,
					sessionCfg,
					runStamp,
					iter,
					arm,
					regularScenarios,
					runsPath,
				)
				if err != nil {
					exitErr(err.Error())
				}
				total += n
			}

			for _, sc := range stressScenarios {
				n, err := runScenarioFlow(
					ctx,
					cfg,
					prov,
					sessionCfg,
					runStamp,
					iter,
					arm,
					[]scenario.Scenario{sc},
					runsPath,
				)
				if err != nil {
					exitErr(err.Error())
				}
				total += n
			}
		}
	}

	manifest := harness.ResultManifest{
		RunStamp:    runStamp,
		CreatedAt:   time.Now().UTC(),
		ProviderID:  prov.ID(),
		ModelID:     cfg.modelID,
		Arms:        arms,
		ScenarioIDs: collectScenarioIDs(scenarios),
		TotalRuns:   total,
		Pricing: harness.PricingSnapshot{
			ProviderID:     prov.ID(),
			ModelID:        cfg.modelID,
			InputPer1KUSD:  cfg.inputPricePer1K,
			OutputPer1KUSD: cfg.outputPricePer1K,
			CapturedAt:     time.Now().UTC(),
			Source:         cfg.pricingSource,
		},
	}
	if manifest.Pricing.Source == "" {
		manifest.Pricing.Source = "manual_flags"
	}
	if err := harness.WriteJSONFile(filepath.Join(outDir, "manifest.json"), manifest); err != nil {
		exitErr(err.Error())
	}

	fmt.Printf("benchrun complete: %s (%d records)\n", outDir, total)
}

type config struct {
	providerID                  string
	modelID                     string
	scenariosDir                string
	resultsRoot                 string
	armsCSV                     string
	systemPrompt                string
	pricingSource               string
	mcpConfigBaselineCSV        string
	mcpConfigDBDenseCSV         string
	mcpConfigDBDenseWithDescCSV string
	mcpConfigDBDenseLHCSV       string
	contextFileBaseline         string
	contextFileDBDense          string
	contextFileDBDenseWithDesc  string
	contextFileDBDenseLH        string
	contextBaseline             string
	contextDBDense              string
	contextDBDenseWithDesc      string
	contextDBDenseLH            string
	claudePermissionMode        string
	runs                        int
	queryTimeout                time.Duration
	strictMCPConfig             bool
	inputPricePer1K             float64
	outputPricePer1K            float64
}

func parseFlags() (config, error) {
	var cfg config

	flag.StringVar(&cfg.providerID, "provider", "claude", "provider id (claude)")
	flag.StringVar(&cfg.modelID, "model", "", "provider model id (required)")
	flag.StringVar(&cfg.scenariosDir, "scenarios", "scenarios", "scenario directory")
	flag.StringVar(&cfg.resultsRoot, "results-root", "results", "results root directory")
	flag.StringVar(&cfg.armsCSV, "arms", "baseline,dbdense,dbdense_lighthouse", "comma-separated benchmark arms")
	flag.StringVar(&cfg.systemPrompt, "system-prompt", "", "optional system prompt")
	flag.StringVar(&cfg.mcpConfigBaselineCSV, "mcp-config-baseline", "", "comma-separated MCP config paths/json for baseline arm")
	flag.StringVar(&cfg.mcpConfigDBDenseCSV, "mcp-config-dbdense", "", "comma-separated MCP config paths/json for dbdense arm")
	flag.StringVar(&cfg.mcpConfigDBDenseWithDescCSV, "mcp-config-dbdense-with-desc", "", "comma-separated MCP config paths/json for dbdense_with_desc arm")
	flag.StringVar(&cfg.mcpConfigDBDenseLHCSV, "mcp-config-dbdense-lighthouse", "", "comma-separated MCP config paths/json for dbdense_lighthouse arm")
	flag.StringVar(&cfg.contextFileBaseline, "context-file-baseline", "", "optional text file injected as context for baseline arm")
	flag.StringVar(&cfg.contextFileDBDense, "context-file-dbdense", "", "optional text file injected as context for dbdense arm")
	flag.StringVar(&cfg.contextFileDBDenseWithDesc, "context-file-dbdense-with-desc", "", "optional text file injected as context for dbdense_with_desc arm")
	flag.StringVar(&cfg.contextFileDBDenseLH, "context-file-dbdense-lighthouse", "", "optional text file injected as context for dbdense_lighthouse arm")
	flag.BoolVar(&cfg.strictMCPConfig, "strict-mcp-config", false, "pass --strict-mcp-config to Claude when MCP configs are set")
	flag.StringVar(&cfg.claudePermissionMode, "claude-permission-mode", "", "optional Claude permission mode (default, acceptEdits, bypassPermissions, dontAsk, plan)")
	flag.IntVar(&cfg.runs, "runs", 1, "repetitions per arm")
	flag.DurationVar(&cfg.queryTimeout, "query-timeout", 5*time.Minute, "per-query timeout")
	flag.Float64Var(&cfg.inputPricePer1K, "input-price-per-1k", 0, "input token price (USD per 1K tokens)")
	flag.Float64Var(&cfg.outputPricePer1K, "output-price-per-1k", 0, "output token price (USD per 1K tokens)")
	flag.StringVar(&cfg.pricingSource, "pricing-source", "", "pricing source note (example: anthropic_pricing_page_2026_02_22)")
	flag.Parse()

	// Default system prompt: same for all arms to ensure fairness.
	if strings.TrimSpace(cfg.systemPrompt) == "" {
		cfg.systemPrompt = "You are connected to a PostgreSQL database. Use the available tools to query it and answer the user's question. Return your final answer as JSON."
	}

	if strings.TrimSpace(cfg.modelID) == "" {
		return cfg, fmt.Errorf("-model is required")
	}
	if cfg.runs <= 0 {
		return cfg, fmt.Errorf("-runs must be >= 1")
	}
	if cfg.queryTimeout <= 0 {
		return cfg, fmt.Errorf("-query-timeout must be > 0")
	}
	baselineContext, err := loadContextFile(cfg.contextFileBaseline)
	if err != nil {
		return cfg, err
	}
	dbdenseContext, err := loadContextFile(cfg.contextFileDBDense)
	if err != nil {
		return cfg, err
	}
	dbdenseWithDescContext, err := loadContextFile(cfg.contextFileDBDenseWithDesc)
	if err != nil {
		return cfg, err
	}
	dbdenseLHContext, err := loadContextFile(cfg.contextFileDBDenseLH)
	if err != nil {
		return cfg, err
	}
	cfg.contextBaseline = baselineContext
	cfg.contextDBDense = dbdenseContext
	cfg.contextDBDenseWithDesc = dbdenseWithDescContext
	cfg.contextDBDenseLH = dbdenseLHContext

	return cfg, nil
}

func parseArms(csv string) ([]string, error) {
	// allowedArmCount is the number of recognized benchmark arm names.
	const allowedArmCount = 6
	allowed := make(map[string]bool, allowedArmCount)
	allowed[armBaseline] = true
	allowed[armDBDense] = true
	allowed[armDBDenseWithDesc] = true
	allowed[armDBDenseLH] = true
	allowed[armWarmupOnly] = true
	allowed[armWarmSlice] = true

	var out []string
	seen := make(map[string]bool, allowedArmCount)
	for _, raw := range strings.Split(csv, ",") {
		arm := strings.TrimSpace(raw)
		if arm == "" {
			continue
		}
		if !allowed[arm] {
			return nil, fmt.Errorf("unsupported arm %q (allowed: %s, %s, %s, %s)", arm, armBaseline, armDBDense, armDBDenseWithDesc, armDBDenseLH)
		}
		if arm == armWarmupOnly || arm == armWarmSlice {
			arm = armDBDense
		}
		if seen[arm] {
			continue
		}
		seen[arm] = true
		out = append(out, arm)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no benchmark arms selected")
	}
	return out, nil
}

func validateArmInputs(cfg config, arms []string) error {
	hasBaseline := false
	armSet := make(map[string]bool, len(arms))
	for _, arm := range arms {
		armSet[arm] = true
		hasBaseline = hasBaseline || arm == armBaseline
	}

	baselineMCP := armHasMCPConfig(cfg, armBaseline)
	for arm := range armSet {
		if arm == armBaseline {
			continue
		}
		if hasBaseline && baselineMCP != armHasMCPConfig(cfg, arm) {
			return fmt.Errorf(
				"for fair MCP mode set both -mcp-config-baseline and %s, or set neither for no-MCP mode",
				armMCPFlag(arm),
			)
		}
	}
	hasAnyMCP := false
	for arm := range armSet {
		hasAnyMCP = hasAnyMCP || armHasMCPConfig(cfg, arm)
	}
	if cfg.strictMCPConfig && !hasAnyMCP {
		return fmt.Errorf("-strict-mcp-config requires MCP configs")
	}

	for arm := range armSet {
		if arm == armBaseline {
			continue
		}
		hasMCP := armHasMCPConfig(cfg, arm)
		hasContext := strings.TrimSpace(armContextPayload(cfg, arm)) != ""
		if !hasMCP && !hasContext {
			return fmt.Errorf("%s arm requires at least one of %s or %s (both allowed)", arm, armMCPFlag(arm), armContextFileFlag(arm))
		}
	}
	return nil
}

func loadContextFile(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", nil
	}

	data, err := os.ReadFile(trimmed)
	if err != nil {
		return "", fmt.Errorf("read context file %q: %w", trimmed, err)
	}
	content := string(data)
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("context file %q is empty", trimmed)
	}
	return content, nil
}

func splitScenarioCategories(in []scenario.Scenario) (regular []scenario.Scenario, stress []scenario.Scenario) {
	for _, sc := range in {
		if sc.Category == scenario.CategoryStress {
			stress = append(stress, sc)
			continue
		}
		regular = append(regular, sc)
	}
	return regular, stress
}

func collectScenarioIDs(in []scenario.Scenario) []string {
	out := make([]string, 0, len(in))
	for _, sc := range in {
		out = append(out, sc.ID)
	}
	sort.Strings(out)
	return out
}

func runScenarioFlow(
	ctx context.Context,
	cfg config,
	prov provider.Provider,
	sessionCfg provider.SessionConfig,
	runStamp string,
	iteration int,
	arm string,
	scenarios []scenario.Scenario,
	runsPath string,
) (int, error) {
	openCtx, cancelOpen := context.WithTimeout(ctx, cfg.queryTimeout)
	defer cancelOpen()

	sess, err := prov.NewSession(openCtx, sessionCfg)
	if err != nil {
		now := time.Now().UTC()
		count := 0
		for _, sc := range scenarios {
			runID := fmt.Sprintf("%s-%s-%02d-%s", runStamp, arm, iteration, sc.ID)
			rec := harness.RunRecord{
				RunID:            runID,
				ScenarioID:       sc.ID,
				ScenarioLabel:    sc.Label,
				ScenarioCategory: sc.Category,
				Arm:              arm,
				ProviderID:       prov.ID(),
				ModelID:          sessionCfg.Model,
				Iteration:        iteration,
				Prompt:           sc.Prompt,
				StartedAt:        now,
				FinishedAt:       now,
				Error:            fmt.Sprintf("session start failed: %v", err),
				Complete:         false,
				Accuracy:         harness.AccuracyMetrics{Score: 0, Pass: false, Missing: []string{"session_start_failed"}},
				IncompleteReasons: []string{
					"session_start_failed",
				},
			}
			if err := harness.AppendJSONL(runsPath, rec); err != nil {
				return count, err
			}
			count++
		}
		return count, nil
	}

	defer func() {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelClose()
		_ = sess.Close(closeCtx)
	}()

	ctxPayload := armContextPayload(cfg, arm)
	count := 0
	for _, sc := range scenarios {
		runID := fmt.Sprintf("%s-%s-%02d-%s", runStamp, arm, iteration, sc.ID)
		start := time.Now().UTC()

		queryCtx, cancelQuery := context.WithTimeout(ctx, cfg.queryTimeout)
		res, qErr := sess.Query(queryCtx, provider.QueryRequest{
			RunID:      runID,
			ScenarioID: sc.ID,
			Prompt:     sc.Prompt,
			MCPContext: ctxPayload,
			Metadata:   queryMetadata(cfg, arm, iteration, len(ctxPayload)),
		})
		cancelQuery()

		finish := time.Now().UTC()
		rec := harness.RunRecord{
			RunID:            runID,
			ScenarioID:       sc.ID,
			ScenarioLabel:    sc.Label,
			ScenarioCategory: sc.Category,
			Arm:              arm,
			ProviderID:       sess.ProviderID(),
			ModelID:          sess.ModelID(),
			SessionID:        sess.ID(),
			Iteration:        iteration,
			Prompt:           sc.Prompt,
			MCPContextBytes:  len(ctxPayload),
			StartedAt:        start,
			FinishedAt:       finish,
			WallLatencyMs:    finish.Sub(start).Milliseconds(),
			Complete:         true,
		}

		if qErr != nil {
			rec.Error = qErr.Error()
			rec.Complete = false
			rec.IncompleteReasons = append(rec.IncompleteReasons, "query_failed")
			if errors.Is(qErr, provider.ErrProviderUnavailable) {
				rec.IncompleteReasons = append(rec.IncompleteReasons, "provider_unavailable")
			}
			if errors.Is(qErr, provider.ErrProviderExecution) {
				rec.IncompleteReasons = append(rec.IncompleteReasons, "provider_execution_error")
			}
			if detectQueryTimeout(qErr.Error()) {
				rec.IncompleteReasons = append(rec.IncompleteReasons, "query_timeout")
			}
			rec.Accuracy = harness.AccuracyMetrics{Score: 0, Pass: false, Missing: []string{"query_failed"}}
		} else {
			if res.Usage.TotalTokens == 0 && (res.Usage.PromptTokens > 0 || res.Usage.CompletionTokens > 0) {
				res.Usage.TotalTokens = res.Usage.PromptTokens + res.Usage.CompletionTokens
			}

			rec.Answer = res.Answer
			rec.Usage = res.Usage
			rec.Timing = res.Timing
			rec.ToolCalls = res.ToolCalls
			rec.PromptChars = res.PromptChars
			rec.EstimatedInputTokens = res.EstimatedInputTokens
			rec.NumTurns = res.NumTurns
			verifyOutcome := verifyScenarioAnswer(sc, res.Answer)
			rec.Accuracy = verifyOutcome.Accuracy
			if verifyOutcome.Error != "" {
				rec.Error = verifyOutcome.Error
			}
			if verifyOutcome.IncompleteReason != "" {
				rec.Complete = false
				rec.IncompleteReasons = append(rec.IncompleteReasons, verifyOutcome.IncompleteReason)
			}

			if tokenCount(rec.Usage) == 0 {
				rec.Complete = false
				rec.IncompleteReasons = append(rec.IncompleteReasons, "missing_usage_tokens")
			}
			if rec.WallLatencyMs <= 0 && rec.Timing.LatencyMs <= 0 {
				rec.Complete = false
				rec.IncompleteReasons = append(rec.IncompleteReasons, "missing_latency")
			}
		}

		if err := harness.AppendJSONL(runsPath, rec); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

func queryMetadata(cfg config, arm string, iteration int, contextBytes int) map[string]string {
	meta := map[string]string{
		"arm":       arm,
		"iteration": fmt.Sprintf("%d", iteration),
	}
	if contextBytes > 0 {
		meta["context_bytes"] = fmt.Sprintf("%d", contextBytes)
	}

	contextFile := strings.TrimSpace(armContextFile(cfg, arm))
	if contextFile != "" {
		meta["context_file"] = contextFile
	}

	mcpConfig := strings.TrimSpace(armMCPConfig(cfg, arm))
	if mcpConfig != "" {
		meta["claude_mcp_config"] = mcpConfig
		if cfg.strictMCPConfig {
			meta["claude_strict_mcp_config"] = "true"
		}
	}
	if strings.TrimSpace(cfg.claudePermissionMode) != "" {
		meta["claude_permission_mode"] = strings.TrimSpace(cfg.claudePermissionMode)
	}
	return meta
}

func armContextPayload(cfg config, arm string) string {
	switch arm {
	case armBaseline:
		return cfg.contextBaseline
	case armDBDense:
		return cfg.contextDBDense
	case armDBDenseWithDesc:
		return cfg.contextDBDenseWithDesc
	case armDBDenseLH:
		return cfg.contextDBDenseLH
	default:
		return ""
	}
}

func armContextFile(cfg config, arm string) string {
	switch arm {
	case armBaseline:
		return cfg.contextFileBaseline
	case armDBDense:
		return cfg.contextFileDBDense
	case armDBDenseWithDesc:
		return cfg.contextFileDBDenseWithDesc
	case armDBDenseLH:
		return cfg.contextFileDBDenseLH
	default:
		return ""
	}
}

func armMCPConfig(cfg config, arm string) string {
	switch arm {
	case armBaseline:
		return cfg.mcpConfigBaselineCSV
	case armDBDense:
		return cfg.mcpConfigDBDenseCSV
	case armDBDenseWithDesc:
		return cfg.mcpConfigDBDenseWithDescCSV
	case armDBDenseLH:
		return cfg.mcpConfigDBDenseLHCSV
	default:
		return ""
	}
}

func armHasMCPConfig(cfg config, arm string) bool {
	return strings.TrimSpace(armMCPConfig(cfg, arm)) != ""
}

func armMCPFlag(arm string) string {
	switch arm {
	case armBaseline:
		return "-mcp-config-baseline"
	case armDBDense:
		return "-mcp-config-dbdense"
	case armDBDenseWithDesc:
		return "-mcp-config-dbdense-with-desc"
	case armDBDenseLH:
		return "-mcp-config-dbdense-lighthouse"
	default:
		return "-mcp-config"
	}
}

func armContextFileFlag(arm string) string {
	switch arm {
	case armBaseline:
		return "-context-file-baseline"
	case armDBDense:
		return "-context-file-dbdense"
	case armDBDenseWithDesc:
		return "-context-file-dbdense-with-desc"
	case armDBDenseLH:
		return "-context-file-dbdense-lighthouse"
	default:
		return "-context-file"
	}
}

func tokenCount(usage provider.UsageMetrics) int {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.PromptTokens + usage.CompletionTokens
}

func detectQueryTimeout(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "deadline exceeded")
}

func exitErr(msg string) {
	fmt.Fprintln(os.Stderr, "benchrun error:", msg)
	os.Exit(1)
}
