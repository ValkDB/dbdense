//go:build integration

package extract_test

import (
	"strings"
	"testing"

	"github.com/valkdb/dbdense/internal/compile"
)

// TestEndToEnd_ExportSlice verifies the full pipeline: extract from Postgres,
// compile/slice for a question, and verify the result is coherent.
func TestEndToEnd_ExportSlice(t *testing.T) {
	export := postgresExport(t)

	c := &compile.Compiler{Export: export}

	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	entityNames := make(map[string]bool, len(result.Entities))
	for _, e := range result.Entities {
		entityNames[e.Name] = true
	}

	if !entityNames["orders"] {
		t.Error("slice should include orders")
	}
	if !entityNames["users"] {
		t.Error("slice should include users (1-hop from orders)")
	}

	// DSL should contain the header and at least one entity.
	if len(result.DSL) < 20 {
		t.Errorf("DSL too short: %q", result.DSL)
	}
	if !strings.HasPrefix(result.DSL, "-- dbdense schema context\n") {
		t.Error("DSL should start with DDL header")
	}
	if !strings.Contains(result.DSL, "CREATE TABLE") {
		t.Error("DSL should contain CREATE TABLE statements")
	}
	if !strings.Contains(result.DSL, "ALTER TABLE orders ADD FOREIGN KEY (user_id) REFERENCES users (id)") {
		t.Error("DSL should contain orders->users FK")
	}

}
