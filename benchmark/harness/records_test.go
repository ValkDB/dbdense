package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRunRecords_AllowsLargeLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.jsonl")
	want := RunRecord{
		RunID:      "run-1",
		ScenarioID: "scenario-1",
		Arm:        "dbdense",
		Answer:     strings.Repeat("a", 70*1024),
	}

	line, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if err := os.WriteFile(path, append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write runs.jsonl: %v", err)
	}

	got, err := LoadRunRecords(path)
	if err != nil {
		t.Fatalf("LoadRunRecords error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if got[0].Answer != want.Answer {
		t.Fatalf("answer length = %d, want %d", len(got[0].Answer), len(want.Answer))
	}
}
