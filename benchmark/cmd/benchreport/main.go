// benchreport aggregates runs.jsonl artifacts into a publishable report with
// summary metrics and acceptance-gate results.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/valkdb/dbdense/benchmark/harness"
)

type statSummary struct {
	Count  int     `json:"count"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	P90    float64 `json:"p90"`
	StdDev float64 `json:"stddev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

type armSummary struct {
	Arm                 string      `json:"arm"`
	Category            string      `json:"category"`
	Runs                int         `json:"runs"`
	CompleteRuns        int         `json:"complete_runs"`
	IncompleteRuns      int         `json:"incomplete_runs"`
	PassRate            float64     `json:"pass_rate"`
	AverageScore        float64     `json:"average_score"`
	CompletionToken     statSummary `json:"completion_token"`
	EstimatedInputToken statSummary `json:"estimated_input_token"`
	TotalReportedToken  statSummary `json:"total_reported_token"`
	LatencyMs           statSummary `json:"latency_ms"`
}

type comparisonSummary struct {
	BaselineArm                    string  `json:"baseline_arm"`
	TargetArm                      string  `json:"target_arm"`
	CompletionTokenReductionPct    float64 `json:"completion_token_reduction_pct"`
	LatencyReductionPct            float64 `json:"latency_reduction_pct"`
	PassRateDelta                  float64 `json:"pass_rate_delta"`
	CompletionTokenPValue          float64 `json:"completion_token_p_value"`
	LatencyPValue                  float64 `json:"latency_p_value"`
	BaselineMedianCompletionTokens float64 `json:"baseline_median_completion_tokens"`
	TargetMedianCompletionTokens   float64 `json:"target_median_completion_tokens"`
	BaselineMedianEstInputTokens   float64 `json:"baseline_median_est_input_tokens"`
	TargetMedianEstInputTokens     float64 `json:"target_median_est_input_tokens"`
	BaselineMedianMs               float64 `json:"baseline_median_ms"`
	TargetMedianMs                 float64 `json:"target_median_ms"`
}

type gateResult struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Pass    bool   `json:"pass"`
	Details string `json:"details"`
}

type report struct {
	GeneratedAt       time.Time                 `json:"generated_at"`
	InputPath         string                    `json:"input_path"`
	RecordsPath       string                    `json:"records_path"`
	TotalRecords      int                       `json:"total_records"`
	CompleteRecords   int                       `json:"complete_records"`
	Arms              []string                  `json:"arms"`
	ScenarioIDs       []string                  `json:"scenario_ids"`
	RegularByArm      map[string]armSummary     `json:"regular_by_arm"`
	StressByArm       map[string]armSummary     `json:"stress_by_arm"`
	Comparison        comparisonSummary         `json:"comparison"`
	Gates             []gateResult              `json:"gates"`
	Publishable       bool                      `json:"publishable"`
	Manifest          *harness.ResultManifest   `json:"manifest,omitempty"`
	CountsPerScenario map[string]map[string]int `json:"counts_per_scenario"`
}

func main() {
	var (
		inputPath string
		targetArm string
		minRuns   int
		outJSON   string
		outMD     string
	)

	flag.StringVar(&inputPath, "input", "", "run directory (results/<stamp>) or path to runs.jsonl")
	flag.StringVar(&targetArm, "target-arm", "dbdense", "arm to compare against baseline")
	flag.IntVar(&minRuns, "min-runs", 3, "minimum complete runs per arm per scenario for gate 2")
	flag.StringVar(&outJSON, "out-json", "report.json", "output JSON filename")
	flag.StringVar(&outMD, "out-md", "report.md", "output Markdown filename")
	flag.Parse()

	if strings.TrimSpace(inputPath) == "" {
		exitErr("-input is required")
	}
	if minRuns <= 0 {
		exitErr("-min-runs must be >= 1")
	}

	runDir, recordsPath, manifestPath, err := resolveInput(inputPath)
	if err != nil {
		exitErr(err.Error())
	}

	records, err := harness.LoadRunRecords(recordsPath)
	if err != nil {
		exitErr(err.Error())
	}
	if len(records) == 0 {
		exitErr("no run records found")
	}

	rep := buildReport(runDir, recordsPath, records, targetArm, minRuns)
	if _, err := os.Stat(manifestPath); err == nil {
		var m harness.ResultManifest
		data, err := os.ReadFile(manifestPath)
		if err == nil && json.Unmarshal(data, &m) == nil {
			rep.Manifest = &m
		}
	}

	if err := writeJSON(filepath.Join(runDir, outJSON), rep); err != nil {
		exitErr(err.Error())
	}
	if err := os.WriteFile(filepath.Join(runDir, outMD), []byte(renderMarkdown(rep)), 0o644); err != nil {
		exitErr(err.Error())
	}

	fmt.Printf("benchreport complete: %s\n", runDir)
	fmt.Printf("publishable: %t\n", rep.Publishable)
}

func resolveInput(input string) (runDir string, recordsPath string, manifestPath string, err error) {
	info, err := os.Stat(input)
	if err != nil {
		return "", "", "", fmt.Errorf("stat %s: %w", input, err)
	}
	if info.IsDir() {
		runDir = input
		recordsPath = filepath.Join(runDir, "runs.jsonl")
		manifestPath = filepath.Join(runDir, "manifest.json")
		return runDir, recordsPath, manifestPath, nil
	}
	if filepath.Base(input) != "runs.jsonl" {
		return "", "", "", fmt.Errorf("input file must be runs.jsonl or a run directory")
	}
	runDir = filepath.Dir(input)
	recordsPath = input
	manifestPath = filepath.Join(runDir, "manifest.json")
	return runDir, recordsPath, manifestPath, nil
}

func buildReport(runDir, recordsPath string, records []harness.RunRecord, targetArm string, minRuns int) report {
	arms := harness.UniqueArms(records)
	scenarioIDs := harness.UniqueScenarioIDs(records)

	complete := 0
	for _, r := range records {
		if r.Complete {
			complete++
		}
	}

	regularByArmRecords := groupByArm(filterCategory(records, "regular"))
	stressByArmRecords := groupByArm(filterCategory(records, "stress"))

	regularSummaries := make(map[string]armSummary, len(regularByArmRecords))
	for arm, rs := range regularByArmRecords {
		regularSummaries[arm] = summarizeArm(arm, "regular", rs)
	}

	stressSummaries := make(map[string]armSummary, len(stressByArmRecords))
	for arm, rs := range stressByArmRecords {
		stressSummaries[arm] = summarizeArm(arm, "stress", rs)
	}

	baselineArm := "baseline"
	if _, ok := regularSummaries[baselineArm]; !ok && len(arms) > 0 {
		baselineArm = arms[0]
	}
	if _, ok := regularSummaries[targetArm]; !ok {
		if _, exists := regularSummaries["dbdense"]; exists {
			targetArm = "dbdense"
		} else if _, exists := regularSummaries["warmup+slice"]; exists {
			targetArm = "warmup+slice"
		} else if _, exists := regularSummaries["warmup-only"]; exists {
			targetArm = "warmup-only"
		}
	}

	comp := compareArms(baselineArm, targetArm, regularByArmRecords)
	counts := countPerScenarioArm(records)
	gates := evaluateGates(records, arms, scenarioIDs, minRuns, baselineArm, targetArm, regularSummaries, stressSummaries, comp, counts)

	publishable := true
	for _, g := range gates {
		if !g.Pass {
			publishable = false
			break
		}
	}

	return report{
		GeneratedAt:       time.Now().UTC(),
		InputPath:         runDir,
		RecordsPath:       recordsPath,
		TotalRecords:      len(records),
		CompleteRecords:   complete,
		Arms:              arms,
		ScenarioIDs:       scenarioIDs,
		RegularByArm:      regularSummaries,
		StressByArm:       stressSummaries,
		Comparison:        comp,
		Gates:             gates,
		Publishable:       publishable,
		CountsPerScenario: counts,
	}
}

func filterCategory(records []harness.RunRecord, category string) []harness.RunRecord {
	var out []harness.RunRecord
	for _, r := range records {
		c := strings.TrimSpace(strings.ToLower(r.ScenarioCategory))
		if c == "" {
			c = "regular"
		}
		if c == category {
			out = append(out, r)
		}
	}
	return out
}

func groupByArm(records []harness.RunRecord) map[string][]harness.RunRecord {
	out := make(map[string][]harness.RunRecord, len(records))
	for _, r := range records {
		out[r.Arm] = append(out[r.Arm], r)
	}
	return out
}

func summarizeArm(arm, category string, records []harness.RunRecord) armSummary {
	s := armSummary{
		Arm:      arm,
		Category: category,
		Runs:     len(records),
	}
	if len(records) == 0 {
		return s
	}

	var (
		completionVals    []float64
		estInputVals      []float64
		totalReportedVals []float64
		latencyVals       []float64
		totalScore        float64
		passCount         int
	)

	for _, r := range records {
		if !r.Complete {
			s.IncompleteRuns++
			continue
		}
		s.CompleteRuns++
		if r.Accuracy.Pass {
			passCount++
		}
		totalScore += r.Accuracy.Score

		completionTokens := float64(r.Usage.CompletionTokens)
		if completionTokens > 0 {
			completionVals = append(completionVals, completionTokens)
		}
		estInput := float64(r.EstimatedInputTokens)
		if estInput > 0 {
			estInputVals = append(estInputVals, estInput)
		}
		totalReported := float64(tokenCountFromRecord(r))
		if totalReported > 0 {
			totalReportedVals = append(totalReportedVals, totalReported)
		}

		latency := float64(latencyMS(r))
		if latency > 0 {
			latencyVals = append(latencyVals, latency)
		}
	}

	if s.CompleteRuns > 0 {
		s.PassRate = float64(passCount) / float64(s.CompleteRuns)
		s.AverageScore = totalScore / float64(s.CompleteRuns)
	}

	s.CompletionToken = summarizeStats(completionVals)
	s.EstimatedInputToken = summarizeStats(estInputVals)
	s.TotalReportedToken = summarizeStats(totalReportedVals)
	s.LatencyMs = summarizeStats(latencyVals)
	return s
}

func compareArms(baselineArm, targetArm string, grouped map[string][]harness.RunRecord) comparisonSummary {
	baseline := completeRecords(grouped[baselineArm])
	target := completeRecords(grouped[targetArm])

	bCompletionTokens := extractMetric(baseline, func(r harness.RunRecord) float64 { return float64(r.Usage.CompletionTokens) })
	tCompletionTokens := extractMetric(target, func(r harness.RunRecord) float64 { return float64(r.Usage.CompletionTokens) })
	bEstInput := extractMetric(baseline, func(r harness.RunRecord) float64 { return float64(r.EstimatedInputTokens) })
	tEstInput := extractMetric(target, func(r harness.RunRecord) float64 { return float64(r.EstimatedInputTokens) })
	bLatency := extractMetric(baseline, func(r harness.RunRecord) float64 { return float64(latencyMS(r)) })
	tLatency := extractMetric(target, func(r harness.RunRecord) float64 { return float64(latencyMS(r)) })

	bCompletionMed := median(bCompletionTokens)
	tCompletionMed := median(tCompletionTokens)
	bEstInputMed := median(bEstInput)
	tEstInputMed := median(tEstInput)
	bLatencyMed := median(bLatency)
	tLatencyMed := median(tLatency)
	completionTokenPValue := mannWhitneyTwoSidedP(bCompletionTokens, tCompletionTokens)
	latencyPValue := mannWhitneyTwoSidedP(bLatency, tLatency)
	if math.IsNaN(completionTokenPValue) || math.IsInf(completionTokenPValue, 0) {
		completionTokenPValue = 1
	}
	if math.IsNaN(latencyPValue) || math.IsInf(latencyPValue, 0) {
		latencyPValue = 1
	}

	comp := comparisonSummary{
		BaselineArm:                    baselineArm,
		TargetArm:                      targetArm,
		BaselineMedianCompletionTokens: bCompletionMed,
		TargetMedianCompletionTokens:   tCompletionMed,
		BaselineMedianEstInputTokens:   bEstInputMed,
		TargetMedianEstInputTokens:     tEstInputMed,
		BaselineMedianMs:               bLatencyMed,
		TargetMedianMs:                 tLatencyMed,
		CompletionTokenPValue:          completionTokenPValue,
		LatencyPValue:                  latencyPValue,
	}

	if bCompletionMed > 0 {
		comp.CompletionTokenReductionPct = (bCompletionMed - tCompletionMed) / bCompletionMed * 100
	}
	if bLatencyMed > 0 {
		comp.LatencyReductionPct = (bLatencyMed - tLatencyMed) / bLatencyMed * 100
	}

	basePassRate := passRate(baseline)
	targetPassRate := passRate(target)
	comp.PassRateDelta = targetPassRate - basePassRate

	return comp
}

func evaluateGates(
	records []harness.RunRecord,
	arms []string,
	scenarioIDs []string,
	minRuns int,
	baselineArm string,
	targetArm string,
	regular map[string]armSummary,
	stress map[string]armSummary,
	comp comparisonSummary,
	counts map[string]map[string]int,
) []gateResult {
	var gates []gateResult

	// Gate 1: no missing token/time/accuracy fields.
	missing := 0
	for _, r := range records {
		if !r.Complete {
			continue
		}
		if tokenCountFromRecord(r) <= 0 || latencyMS(r) <= 0 || r.Accuracy.Score < 0 || r.Accuracy.Score > 1 {
			missing++
		}
	}
	gates = append(gates, gateResult{
		ID:      1,
		Name:    "No missing token/time/accuracy fields",
		Pass:    missing == 0,
		Details: fmt.Sprintf("missing=%d", missing),
	})

	// Gate 2: >= min runs per arm per scenario.
	failures := 0
	for _, arm := range arms {
		for _, scenarioID := range scenarioIDs {
			if counts[arm][scenarioID] < minRuns {
				failures++
			}
		}
	}
	gates = append(gates, gateResult{
		ID:      2,
		Name:    fmt.Sprintf(">= %d complete runs per arm per scenario", minRuns),
		Pass:    failures == 0,
		Details: fmt.Sprintf("underfilled_cells=%d", failures),
	})

	baseRegular := regular[baselineArm]
	targetRegular := regular[targetArm]

	// Gate 3: accuracy not lower than baseline.
	gates = append(gates, gateResult{
		ID:      3,
		Name:    "Target accuracy is not lower than baseline",
		Pass:    targetRegular.PassRate >= baseRegular.PassRate,
		Details: fmt.Sprintf("target_pass_rate=%.4f baseline_pass_rate=%.4f", targetRegular.PassRate, baseRegular.PassRate),
	})

	// Gate 4: completion token overhead — dbdense context should not cause
	// significantly more verbose output than baseline.
	const maxCompletionOverheadPct = 25.0 // maximum acceptable completion token increase vs baseline
	completionOverheadPct := 0.0
	if comp.BaselineMedianCompletionTokens > 0 {
		completionOverheadPct = (comp.TargetMedianCompletionTokens - comp.BaselineMedianCompletionTokens) / comp.BaselineMedianCompletionTokens * 100
	}
	gates = append(gates, gateResult{
		ID:      4,
		Name:    "Completion token overhead <= 25% vs baseline",
		Pass:    completionOverheadPct <= maxCompletionOverheadPct,
		Details: fmt.Sprintf("completion_overhead_pct=%.2f baseline_median=%.0f target_median=%.0f", completionOverheadPct, comp.BaselineMedianCompletionTokens, comp.TargetMedianCompletionTokens),
	})

	// Gate 5: time reduction >= 15%.
	gates = append(gates, gateResult{
		ID:      5,
		Name:    "Latency reduction >= 15% (median)",
		Pass:    comp.LatencyReductionPct >= 15,
		Details: fmt.Sprintf("latency_reduction_pct=%.2f", comp.LatencyReductionPct),
	})

	// Gate 6: session continuity.
	continuityFailures, continuityDetails := sessionContinuityFailures(records)
	gates = append(gates, gateResult{
		ID:      6,
		Name:    "Session continuity preserved per arm iteration",
		Pass:    continuityFailures == 0,
		Details: continuityDetails,
	})

	// Gate 7: stress gate.
	baseStress := stress[baselineArm]
	targetStress := stress[targetArm]
	baseStressFail := baseStress.PassRate < 0.5 || baseStress.AverageScore < 0.5
	targetStressPass := targetStress.PassRate >= 0.9
	gates = append(gates, gateResult{
		ID:      7,
		Name:    "Stress gate: target >= 90% pass rate and baseline fails",
		Pass:    targetStressPass && baseStressFail,
		Details: fmt.Sprintf("target_pass_rate=%.4f baseline_pass_rate=%.4f", targetStress.PassRate, baseStress.PassRate),
	})

	return gates
}

func countPerScenarioArm(records []harness.RunRecord) map[string]map[string]int {
	out := make(map[string]map[string]int, len(records))
	for _, r := range records {
		if !r.Complete {
			continue
		}
		if out[r.Arm] == nil {
			out[r.Arm] = make(map[string]int, len(records))
		}
		out[r.Arm][r.ScenarioID]++
	}
	return out
}

func completeRecords(in []harness.RunRecord) []harness.RunRecord {
	var out []harness.RunRecord
	for _, r := range in {
		if r.Complete {
			out = append(out, r)
		}
	}
	return out
}

func tokenCountFromRecord(r harness.RunRecord) int {
	if r.Usage.TotalTokens > 0 {
		return r.Usage.TotalTokens
	}
	return r.Usage.PromptTokens + r.Usage.CompletionTokens
}

func latencyMS(r harness.RunRecord) int64 {
	if r.WallLatencyMs > 0 {
		return r.WallLatencyMs
	}
	return r.Timing.LatencyMs
}

func passRate(records []harness.RunRecord) float64 {
	if len(records) == 0 {
		return 0
	}
	pass := 0
	for _, r := range records {
		if r.Accuracy.Pass {
			pass++
		}
	}
	return float64(pass) / float64(len(records))
}

func sessionContinuityFailures(records []harness.RunRecord) (int, string) {
	type group struct {
		completeRuns    int
		missingSession  int
		distinctSession map[string]bool
	}

	groups := make(map[string]*group, len(records))
	keys := make([]string, 0, len(records))

	for _, r := range records {
		if !r.Complete {
			continue
		}
		key := fmt.Sprintf("%s#%d", r.Arm, r.Iteration)
		g := groups[key]
		if g == nil {
			g = &group{distinctSession: make(map[string]bool, 1)}
			groups[key] = g
			keys = append(keys, key)
		}
		g.completeRuns++
		sid := strings.TrimSpace(r.SessionID)
		if sid == "" {
			g.missingSession++
			continue
		}
		g.distinctSession[sid] = true
	}

	sort.Strings(keys)
	failures := 0
	var bad []string
	for _, key := range keys {
		g := groups[key]
		if g == nil || g.completeRuns == 0 {
			continue
		}
		if g.missingSession > 0 || len(g.distinctSession) != 1 {
			failures++
			bad = append(bad, fmt.Sprintf(
				"%s(session_ids=%d,missing=%d,complete=%d)",
				key,
				len(g.distinctSession),
				g.missingSession,
				g.completeRuns,
			))
		}
	}

	details := fmt.Sprintf("bad_groups=%d", failures)
	if len(bad) > 0 {
		details += " " + strings.Join(bad, ";")
	}
	return failures, details
}

func extractMetric(records []harness.RunRecord, fn func(harness.RunRecord) float64) []float64 {
	var out []float64
	for _, r := range records {
		v := fn(r)
		if v > 0 {
			out = append(out, v)
		}
	}
	return out
}

func summarizeStats(values []float64) statSummary {
	if len(values) == 0 {
		return statSummary{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	var variance float64
	for _, v := range sorted {
		d := v - mean
		variance += d * d
	}
	// Use sample variance (Bessel's correction: n-1) since benchmark runs
	// are samples, not the full population. At n=3 this avoids underestimating
	// variance by ~33%.
	if len(sorted) > 1 {
		variance /= float64(len(sorted) - 1)
	}

	return statSummary{
		Count:  len(sorted),
		Mean:   mean,
		Median: median(sorted),
		P90:    percentileNearestRank(sorted, 0.90),
		StdDev: math.Sqrt(variance),
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
	}
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func percentileNearestRank(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func mannWhitneyTwoSidedP(a, b []float64) float64 {
	n1 := len(a)
	n2 := len(b)
	if n1 < 2 || n2 < 2 {
		return math.NaN()
	}

	type row struct {
		value float64
		group int
	}
	rows := make([]row, 0, n1+n2)
	for _, v := range a {
		rows = append(rows, row{value: v, group: 0})
	}
	for _, v := range b {
		rows = append(rows, row{value: v, group: 1})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].value < rows[j].value })

	ranks := make([]float64, len(rows))
	tieCounts := []int{}
	for i := 0; i < len(rows); {
		j := i + 1
		for j < len(rows) && rows[j].value == rows[i].value {
			j++
		}
		avgRank := (float64(i+1) + float64(j)) / 2.0
		for k := i; k < j; k++ {
			ranks[k] = avgRank
		}
		tieCounts = append(tieCounts, j-i)
		i = j
	}

	rankSumA := 0.0
	for i := range rows {
		if rows[i].group == 0 {
			rankSumA += ranks[i]
		}
	}

	u1 := rankSumA - (float64(n1*(n1+1)) / 2.0)
	u2 := float64(n1*n2) - u1
	u := math.Min(u1, u2)

	n := float64(n1 + n2)
	mu := float64(n1*n2) / 2.0

	tieTerm := 0.0
	for _, t := range tieCounts {
		tf := float64(t)
		tieTerm += tf*tf*tf - tf
	}
	variance := float64(n1*n2) / 12.0
	if n > 1 {
		variance *= (n + 1.0) - (tieTerm / (n * (n - 1.0)))
	}
	if variance <= 0 {
		return 1
	}
	sigma := math.Sqrt(variance)

	// Continuity correction for two-sided test with U=min(U1, U2).
	z := (u - mu + 0.5) / sigma
	p := math.Erfc(math.Abs(z) / math.Sqrt2)
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

func writeJSON(path string, v any) error {
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

func renderMarkdown(r report) string {
	var b strings.Builder

	fmt.Fprintln(&b, "# Benchmark Report")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- Generated at: `%s`\n", r.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- Input: `%s`\n", r.InputPath)
	fmt.Fprintf(&b, "- Records: `%d` (`%d` complete)\n", r.TotalRecords, r.CompleteRecords)
	fmt.Fprintf(&b, "- Baseline arm: `%s`\n", r.Comparison.BaselineArm)
	fmt.Fprintf(&b, "- Target arm: `%s`\n", r.Comparison.TargetArm)
	fmt.Fprintf(&b, "- Publishable: `%t`\n", r.Publishable)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Regular Summary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Arm | Runs | Complete | Pass Rate | Median Completion Tokens | Median Est. Input Tokens | Median Latency (ms) |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|")
	arms := make([]string, 0, len(r.RegularByArm))
	for arm := range r.RegularByArm {
		arms = append(arms, arm)
	}
	sort.Strings(arms)
	for _, arm := range arms {
		s := r.RegularByArm[arm]
		fmt.Fprintf(&b, "| %s | %d | %d | %.3f | %.0f | %.0f | %.0f |\n",
			arm, s.Runs, s.CompleteRuns, s.PassRate, s.CompletionToken.Median, s.EstimatedInputToken.Median, s.LatencyMs.Median)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Stress Summary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Arm | Runs | Complete | Pass Rate | Avg Score |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|")
	arms = arms[:0]
	for arm := range r.StressByArm {
		arms = append(arms, arm)
	}
	sort.Strings(arms)
	for _, arm := range arms {
		s := r.StressByArm[arm]
		fmt.Fprintf(&b, "| %s | %d | %d | %.3f | %.3f |\n",
			arm, s.Runs, s.CompleteRuns, s.PassRate, s.AverageScore)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Baseline vs Target")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- Completion token reduction (median): `%.2f%%`\n", r.Comparison.CompletionTokenReductionPct)
	fmt.Fprintf(&b, "- Baseline est. input tokens (median): `%.0f`\n", r.Comparison.BaselineMedianEstInputTokens)
	fmt.Fprintf(&b, "- Target est. input tokens (median): `%.0f`\n", r.Comparison.TargetMedianEstInputTokens)
	fmt.Fprintf(&b, "- Latency reduction (median): `%.2f%%`\n", r.Comparison.LatencyReductionPct)
	fmt.Fprintf(&b, "- Pass-rate delta: `%.4f`\n", r.Comparison.PassRateDelta)
	fmt.Fprintf(&b, "- Completion token p-value (Mann-Whitney U): `%.6f`\n", r.Comparison.CompletionTokenPValue)
	fmt.Fprintf(&b, "- Latency p-value (Mann-Whitney U): `%.6f`\n", r.Comparison.LatencyPValue)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Acceptance Gates")
	fmt.Fprintln(&b)
	for _, g := range r.Gates {
		state := "FAIL"
		if g.Pass {
			state = "PASS"
		}
		fmt.Fprintf(&b, "- [%s] G%d %s: %s\n", state, g.ID, g.Name, g.Details)
	}
	fmt.Fprintln(&b)

	return b.String()
}

func exitErr(msg string) {
	fmt.Fprintln(os.Stderr, "benchreport error:", msg)
	os.Exit(1)
}
