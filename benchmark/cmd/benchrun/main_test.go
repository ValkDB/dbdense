package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArmsNormalizesLegacyAliases(t *testing.T) {
	arms, err := parseArms("baseline,warmup-only,warmup+slice,dbdense")
	if err != nil {
		t.Fatalf("parseArms returned error: %v", err)
	}
	if len(arms) != 2 {
		t.Fatalf("expected 2 arms, got %d (%v)", len(arms), arms)
	}
	if arms[0] != armBaseline || arms[1] != armDBDense {
		t.Fatalf("unexpected normalized arms: %v", arms)
	}
}

func TestParseArmsSupportsDBDenseWithDesc(t *testing.T) {
	arms, err := parseArms("baseline,dbdense_with_desc")
	if err != nil {
		t.Fatalf("parseArms returned error: %v", err)
	}
	if len(arms) != 2 {
		t.Fatalf("expected 2 arms, got %d (%v)", len(arms), arms)
	}
	if arms[0] != armBaseline || arms[1] != armDBDenseWithDesc {
		t.Fatalf("unexpected normalized arms: %v", arms)
	}
}

func TestParseArmsSupportsDBDenseLighthouse(t *testing.T) {
	arms, err := parseArms("baseline,dbdense_lighthouse")
	if err != nil {
		t.Fatalf("parseArms returned error: %v", err)
	}
	if len(arms) != 2 {
		t.Fatalf("expected 2 arms, got %d (%v)", len(arms), arms)
	}
	if arms[0] != armBaseline || arms[1] != armDBDenseLH {
		t.Fatalf("unexpected normalized arms: %v", arms)
	}
}

func TestValidateArmInputsRequiresDBDenseSignal(t *testing.T) {
	err := validateArmInputs(config{}, []string{armBaseline, armDBDense})
	if err == nil {
		t.Fatal("expected error when dbdense has neither MCP config nor context file")
	}
	if !strings.Contains(err.Error(), "dbdense arm requires") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateArmInputsRequiresDBDenseWithDescSignal(t *testing.T) {
	err := validateArmInputs(config{}, []string{armBaseline, armDBDenseWithDesc})
	if err == nil {
		t.Fatal("expected error when dbdense_with_desc has neither MCP config nor context file")
	}
	if !strings.Contains(err.Error(), "dbdense_with_desc arm requires") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateArmInputsRequiresDBDenseLighthouseSignal(t *testing.T) {
	err := validateArmInputs(config{}, []string{armBaseline, armDBDenseLH})
	if err == nil {
		t.Fatal("expected error when dbdense_lighthouse has neither MCP config nor context file")
	}
	if !strings.Contains(err.Error(), "dbdense_lighthouse arm requires") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateArmInputsAllowsNoMCPWithContextFile(t *testing.T) {
	cfg := config{contextDBDense: "-- dbdense schema context\nCREATE TABLE users (\n  id uuid PRIMARY KEY\n);\n"}
	if err := validateArmInputs(cfg, []string{armBaseline, armDBDense}); err != nil {
		t.Fatalf("validateArmInputs returned error: %v", err)
	}
}

func TestValidateArmInputsAllowsNoMCPWithContextFilesForAllDenseArms(t *testing.T) {
	cfg := config{
		contextDBDense:         "-- dbdense schema context\nCREATE TABLE users (\n  id uuid PRIMARY KEY\n);\n",
		contextDBDenseWithDesc: "-- dbdense schema context\nCREATE TABLE orders ( -- Orders with business rules.\n  id uuid PRIMARY KEY\n);\n",
		contextDBDenseLH:       "# lighthouse.v0\nT:users|J:orders\nT:orders|J:users\n",
	}
	if err := validateArmInputs(cfg, []string{armBaseline, armDBDense, armDBDenseWithDesc, armDBDenseLH}); err != nil {
		t.Fatalf("validateArmInputs returned error: %v", err)
	}
}

func TestValidateArmInputsEnforcesMCPParity(t *testing.T) {
	cfg := config{mcpConfigBaselineCSV: "baseline.json"}
	err := validateArmInputs(cfg, []string{armBaseline, armDBDense})
	if err == nil {
		t.Fatal("expected error for one-sided MCP configuration")
	}
	if !strings.Contains(err.Error(), "fair MCP mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateArmInputsEnforcesMCPParityWithDBDenseWithDesc(t *testing.T) {
	cfg := config{mcpConfigBaselineCSV: "baseline.json"}
	err := validateArmInputs(cfg, []string{armBaseline, armDBDenseWithDesc})
	if err == nil {
		t.Fatal("expected error for one-sided MCP configuration")
	}
	if !strings.Contains(err.Error(), "-mcp-config-dbdense-with-desc") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateArmInputsEnforcesMCPParityWithDBDenseLighthouse(t *testing.T) {
	cfg := config{mcpConfigBaselineCSV: "baseline.json"}
	err := validateArmInputs(cfg, []string{armBaseline, armDBDenseLH})
	if err == nil {
		t.Fatal("expected error for one-sided MCP configuration")
	}
	if !strings.Contains(err.Error(), "-mcp-config-dbdense-lighthouse") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadContextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "context.txt")
	expected := "-- dbdense schema context\nCREATE TABLE users (\n  id uuid PRIMARY KEY\n);\n"
	if err := os.WriteFile(path, []byte(expected), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := loadContextFile(path)
	if err != nil {
		t.Fatalf("loadContextFile returned error: %v", err)
	}
	if got != expected {
		t.Fatalf("unexpected content:\nwant=%q\ngot=%q", expected, got)
	}
}

func TestLoadContextFileRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("   \n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := loadContextFile(path)
	if err == nil {
		t.Fatal("expected error for empty context file")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}
