package main

import (
	"testing"

	"github.com/valkdb/dbdense/benchmark/scenario"
)

func testScenario(expected map[string]float64) scenario.Scenario {
	return scenario.Scenario{
		ID:    "qx",
		Label: "test",
		Verification: scenario.Verification{
			Response: &scenario.ResponseVerification{
				NumericEquals: expected,
			},
		},
	}
}

func TestVerifyScenarioAnswerPassRawJSON(t *testing.T) {
	sc := testScenario(map[string]float64{"count": 49})
	out := verifyScenarioAnswer(sc, `{"count":49}`)
	if !out.Accuracy.Pass {
		t.Fatalf("expected pass, got %+v", out)
	}
}

func TestVerifyScenarioAnswerPassFencedJSON(t *testing.T) {
	sc := testScenario(map[string]float64{"count": 394, "group_count": 2})
	answer := "Result:\n```json\n{\"count\":394,\"group_count\":2}\n```"
	out := verifyScenarioAnswer(sc, answer)
	if !out.Accuracy.Pass {
		t.Fatalf("expected pass, got %+v", out)
	}
	if out.IncompleteReason != "" {
		t.Fatalf("unexpected incomplete reason: %s", out.IncompleteReason)
	}
}

func TestVerifyScenarioAnswerMismatch(t *testing.T) {
	sc := testScenario(map[string]float64{"count": 49})
	out := verifyScenarioAnswer(sc, `{"count":40}`)
	if out.Accuracy.Pass {
		t.Fatalf("expected fail, got %+v", out)
	}
	if out.IncompleteReason != "" {
		t.Fatalf("mismatch should not mark incomplete: %+v", out)
	}
}

func TestVerifyScenarioAnswerMissingJSON(t *testing.T) {
	sc := testScenario(map[string]float64{"count": 49})
	out := verifyScenarioAnswer(sc, "I cannot access the database.")
	if out.Accuracy.Pass {
		t.Fatalf("expected fail, got %+v", out)
	}
	if out.IncompleteReason != "candidate_response_missing" {
		t.Fatalf("expected candidate_response_missing, got %+v", out)
	}
}
