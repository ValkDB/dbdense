package compile

import (
	"strings"
	"testing"

	"github.com/valkdb/dbdense/pkg/schema"
)

func TestCompileLighthouse(t *testing.T) {
	c := &Compiler{Export: testExport()}
	result, err := c.CompileLighthouse()
	if err != nil {
		t.Fatalf("CompileLighthouse error: %v", err)
	}

	if len(result.Entities) != 4 {
		t.Errorf("expected 4 entities, got %d", len(result.Entities))
	}

	if !strings.HasPrefix(result.DSL, "# lighthouse.v0\n") {
		t.Error("DSL should start with lighthouse header")
	}

	if !strings.Contains(result.DSL, "T:users|J:orders") {
		t.Errorf("expected users with join to orders, got:\n%s", result.DSL)
	}

	if !strings.Contains(result.DSL, "T:orders|J:users") {
		t.Errorf("expected orders with join to users, got:\n%s", result.DSL)
	}

	// audit_log has no FKs, should appear without J: segment.
	if !strings.Contains(result.DSL, "T:audit_log\n") {
		t.Errorf("expected audit_log without joins, got:\n%s", result.DSL)
	}

	// Should NOT contain column details.
	if strings.Contains(result.DSL, "f=") || strings.Contains(result.DSL, "pk=") {
		t.Error("lighthouse should not contain column details")
	}
}

func TestCompileLighthouse_NilExport(t *testing.T) {
	c := &Compiler{Export: nil}
	_, err := c.CompileLighthouse()
	if err == nil {
		t.Error("expected error for nil export")
	}
}

func TestCompileLighthouse_NoEdges(t *testing.T) {
	export := &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{Name: "standalone", Type: "table"},
		},
		Edges: nil,
	}
	c := &Compiler{Export: export}
	result, err := c.CompileLighthouse()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.DSL, "T:standalone\n") {
		t.Errorf("expected standalone table without joins, got:\n%s", result.DSL)
	}
}

func TestCompileLighthouse_MultipleJoins(t *testing.T) {
	export := &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{Name: "users", Type: "table"},
			{Name: "orders", Type: "table"},
			{Name: "payments", Type: "table"},
			{Name: "shipments", Type: "table"},
		},
		Edges: []schema.Edge{
			{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"},
			{FromEntity: "payments", FromField: "order_id", ToEntity: "orders", ToField: "id"},
			{FromEntity: "shipments", FromField: "order_id", ToEntity: "orders", ToField: "id"},
		},
	}
	c := &Compiler{Export: export}
	result, err := c.CompileLighthouse()
	if err != nil {
		t.Fatal(err)
	}

	// orders should join to payments, shipments, users (sorted).
	if !strings.Contains(result.DSL, "T:orders|J:payments,shipments,users") {
		t.Errorf("expected orders with multiple joins, got:\n%s", result.DSL)
	}
}

func TestCompileLighthouse_Legend(t *testing.T) {
	c := &Compiler{Export: testExport()}
	result, err := c.CompileLighthouse()
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.SplitN(result.DSL, "\n", 3)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}
	if lines[0] != "# lighthouse.v0" {
		t.Errorf("first line should be header, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "slice tool") {
		t.Errorf("legend should reference slice tool, got %q", lines[1])
	}
}

func TestLighthouse_TokenEfficiency(t *testing.T) {
	// Verify that lighthouse is significantly smaller than full DDL.
	c := &Compiler{Export: testExport()}
	full, err := c.CompileAll()
	if err != nil {
		t.Fatal(err)
	}
	lh, err := c.CompileLighthouse()
	if err != nil {
		t.Fatal(err)
	}

	if len(lh.DSL) >= len(full.DSL) {
		t.Errorf("lighthouse (%d bytes) should be smaller than full DDL (%d bytes)",
			len(lh.DSL), len(full.DSL))
	}
}

func TestCompileLighthouse_EmbeddedDocs(t *testing.T) {
	export := &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{
				Name: "orders",
				Type: "collection",
				Fields: []schema.Field{
					{Name: "_id", Type: "objectId", IsPK: true},
					{Name: "user_id", Type: "objectId"},
					{Name: "payload", Type: "object", Subfields: []schema.Field{
						{Name: "channel", Type: "string"},
						{Name: "status", Type: "string"},
					}},
					{Name: "shipping", Type: "object", Subfields: []schema.Field{
						{Name: "address", Type: "string"},
					}},
				},
			},
			{
				Name: "users",
				Type: "collection",
				Fields: []schema.Field{
					{Name: "_id", Type: "objectId", IsPK: true},
					{Name: "name", Type: "string"},
				},
			},
		},
		Edges: []schema.Edge{
			{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "_id"},
		},
	}

	c := &Compiler{Export: export}
	result, err := c.CompileLighthouse()
	if err != nil {
		t.Fatal(err)
	}

	// orders has embedded docs (payload, shipping) and joins.
	if !strings.Contains(result.DSL, "T:orders|E:payload,shipping|J:users") {
		t.Errorf("expected orders with embedded docs and joins, got:\n%s", result.DSL)
	}

	// users has no embedded docs.
	if strings.Contains(result.DSL, "T:users|E:") {
		t.Errorf("users should not have embedded doc section, got:\n%s", result.DSL)
	}

	// Legend should mention E=embedded docs.
	if !strings.Contains(result.DSL, "E=embedded docs") {
		t.Errorf("legend should mention E=embedded docs, got:\n%s", result.DSL)
	}
}

func TestNeighbors(t *testing.T) {
	edges := []schema.Edge{
		{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"},
		{FromEntity: "payments", FromField: "order_id", ToEntity: "orders", ToField: "id"},
	}
	g := BuildGraph(edges)

	got := g.Neighbors("orders")
	want := []string{"payments", "users"}
	if !sliceEqual(got, want) {
		t.Errorf("Neighbors(orders) = %v, want %v", got, want)
	}

	got = g.Neighbors("nonexistent")
	if got != nil {
		t.Errorf("Neighbors(nonexistent) = %v, want nil", got)
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
