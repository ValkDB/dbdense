package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/valkdb/dbdense/benchmark/harness"
	"github.com/valkdb/dbdense/benchmark/scenario"
)

const numericTolerance = 1e-9

type verificationOutcome struct {
	Accuracy         harness.AccuracyMetrics
	IncompleteReason string
	Error            string
}

func verifyScenarioAnswer(sc scenario.Scenario, answer string) verificationOutcome {
	obj, err := parseResponseObject(answer)
	if err != nil {
		return verificationOutcome{
			Accuracy: harness.AccuracyMetrics{
				Score:   0,
				Pass:    false,
				Missing: []string{"candidate_response_missing"},
			},
			IncompleteReason: "candidate_response_missing",
			Error:            fmt.Sprintf("candidate response missing valid JSON object: %v", err),
		}
	}

	expected := sc.Verification.Response.NumericEquals
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	matched := make([]string, 0, len(keys))
	missing := make([]string, 0, len(keys))
	for _, key := range keys {
		want := expected[key]
		raw, ok := obj[key]
		if !ok {
			missing = append(missing, fmt.Sprintf("%s missing", key))
			continue
		}

		got, ok := toFloat(raw)
		if !ok {
			missing = append(missing, fmt.Sprintf("%s not numeric", key))
			continue
		}

		if math.Abs(got-want) <= numericTolerance {
			matched = append(matched, fmt.Sprintf("%s=%g", key, want))
			continue
		}
		missing = append(missing, fmt.Sprintf("%s expected=%g actual=%g", key, want, got))
	}

	score := 0.0
	if len(keys) > 0 {
		score = float64(len(matched)) / float64(len(keys))
	}

	return verificationOutcome{
		Accuracy: harness.AccuracyMetrics{
			Score:   score,
			Pass:    len(missing) == 0,
			Matched: matched,
			Missing: missing,
		},
	}
}

func parseResponseObject(answer string) (map[string]any, error) {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return nil, fmt.Errorf("empty answer")
	}

	candidates := []string{trimmed}
	candidates = append(candidates, fencedJSONCandidates(trimmed)...)
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			candidates = append(candidates, strings.TrimSpace(trimmed[start:end+1]))
		}
	}

	seen := make(map[string]bool, len(candidates))
	for _, cand := range candidates {
		c := strings.TrimSpace(stripFenceLanguage(cand))
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		obj, err := decodeJSONObject(c)
		if err == nil {
			return obj, nil
		}
	}

	return nil, fmt.Errorf("no decodable JSON object found")
}

func fencedJSONCandidates(answer string) []string {
	var out []string
	rest := answer
	for {
		start := strings.Index(rest, "```")
		if start < 0 {
			return out
		}
		rest = rest[start+3:]
		end := strings.Index(rest, "```")
		if end < 0 {
			return out
		}
		block := strings.TrimSpace(rest[:end])
		if block != "" {
			out = append(out, block)
		}
		rest = rest[end+3:]
	}
}

func stripFenceLanguage(block string) string {
	trimmed := strings.TrimSpace(block)
	if trimmed == "" {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	if idx := strings.IndexByte(trimmed, '\n'); idx > 0 {
		first := strings.TrimSpace(trimmed[:idx])
		if isLanguageTag(first) {
			return strings.TrimSpace(trimmed[idx+1:])
		}
	}
	return trimmed
}

func isLanguageTag(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func decodeJSONObject(raw string) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}

	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing non-JSON content")
		}
		return nil, err
	}
	return obj, nil
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint64:
		return float64(t), true
	case uint32:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
