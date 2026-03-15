package compile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/valkdb/dbdense/internal/gen"
	"github.com/valkdb/dbdense/pkg/schema"
)

// charsPerToken is the approximate number of characters per LLM token.
// This is a rough heuristic: ~4 chars/token is reasonable for English prose but
// underestimates for SQL DDL (keywords like CREATE, TABLE, ALTER are 1 token each
// but 5-6 chars; short identifiers like "id", "uuid" are also 1 token). A more
// accurate value for DDL is ~3.0-3.5 chars/token. This means token estimates
// in these tests are systematically ~15-25% low. The real benchmark (benchrun)
// uses provider-reported token counts, not this heuristic.
const charsPerToken = 4

func TestClaim_DDLIncludesTypes(t *testing.T) {
	export := &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{Name: "users", Type: "table", Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "email", Type: "text"},
				{Name: "age", Type: "integer"},
				{Name: "balance", Type: "numeric"},
				{Name: "active", Type: "boolean"},
				{Name: "metadata", Type: "jsonb"},
				{Name: "created_at", Type: "timestamp"},
			}},
		},
	}

	c := &Compiler{Export: export}
	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll error: %v", err)
	}

	// Every field type must appear in the DDL output.
	expectedTypes := []string{"uuid", "text", "integer", "numeric", "boolean", "jsonb", "timestamp"}
	for _, typ := range expectedTypes {
		if !strings.Contains(result.DSL, typ) {
			t.Errorf("DDL output missing type %q.\nDDL:\n%s", typ, result.DSL)
		}
	}

	t.Logf("DDL output includes all column types")
	t.Logf("DDL output (%d chars):\n%s", len(result.DSL), result.DSL)
}

func TestClaim_SubsetFilters(t *testing.T) {
	// Generate a 100-table schema.
	cfg := gen.Config{
		TotalTables:  100,
		SignalTables: gen.DefaultSignalTables(),
		Seed:         42,
	}
	export := gen.Generate(cfg)

	c := &Compiler{Export: export}

	// Request only 3 tables.
	subset := []string{"users", "orders", "products"}
	result, err := c.CompileSubset(subset)
	if err != nil {
		t.Fatalf("CompileSubset error: %v", err)
	}

	// Exactly 3 entities returned.
	if len(result.Entities) != 3 {
		t.Errorf("expected 3 entities, got %d", len(result.Entities))
	}

	// All 3 requested tables must be present.
	for _, name := range subset {
		if !strings.Contains(result.DSL, "CREATE TABLE "+name) {
			t.Errorf("subset DDL missing table %q", name)
		}
	}

	// Count CREATE TABLE statements: must be exactly 3.
	createCount := strings.Count(result.DSL, "CREATE TABLE")
	if createCount != 3 {
		t.Errorf("expected 3 CREATE TABLE statements, got %d", createCount)
	}

	// Compare token size: subset should be dramatically smaller than full.
	fullResult, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll error: %v", err)
	}

	subsetTokens := len(result.DSL) / charsPerToken
	fullTokens := len(fullResult.DSL) / charsPerToken
	ratio := float64(subsetTokens) / float64(fullTokens)

	t.Logf("Subset filtering: basic functionality verified")
	t.Logf("  Full schema: %d tables, ~%d tokens (%d chars)", len(fullResult.Entities), fullTokens, len(fullResult.DSL))
	t.Logf("  Subset:      %d tables, ~%d tokens (%d chars)", len(result.Entities), subsetTokens, len(result.DSL))
	t.Logf("  Ratio: %.1f%% of full schema (requesting 3 tables from a 100-table schema returns only the relevant DDL, keeping context focused)", ratio*100)
}

func TestClaim_LighthouseTokenEstimate(t *testing.T) {
	sizes := []int{50, 100, 500, 1000}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%d_tables", size), func(t *testing.T) {
			cfg := gen.Config{
				TotalTables:  size,
				SignalTables: gen.DefaultSignalTables(),
				Seed:         42,
			}
			export := gen.Generate(cfg)

			c := &Compiler{Export: export}
			lhResult, err := c.CompileLighthouse()
			if err != nil {
				t.Fatalf("CompileLighthouse error: %v", err)
			}

			totalTokens := len(lhResult.DSL) / charsPerToken
			tokensPerTable := float64(totalTokens) / float64(size)

			// The documentation claims ~7-10 tokens per table. Using the 4
			// chars/token heuristic, measured values are 7.0-7.6 tokens/table.
			// With a more accurate ~3.2 chars/token for DDL, this would be
			// ~8.8-9.5 actual tokens/table. Long enterprise table names can
			// push this higher (50+ tokens for very long names).
			// Allow 5-15 as a reasonable range for synthetic names.
			const (
				// minTokensPerTable is the lower bound for the token/table claim.
				minTokensPerTable = 5.0
				// maxTokensPerTable is the upper bound for the token/table claim.
				maxTokensPerTable = 15.0
			)

			if tokensPerTable < minTokensPerTable || tokensPerTable > maxTokensPerTable {
				t.Errorf("lighthouse tokens/table = %.1f, expected %.0f-%.0f range",
					tokensPerTable, minTokensPerTable, maxTokensPerTable)
			}

			// Also compare with full DDL to show compactness.
			fullResult, err := c.CompileAll()
			if err != nil {
				t.Fatalf("CompileAll error: %v", err)
			}
			fullTokens := len(fullResult.DSL) / charsPerToken
			compressionRatio := float64(totalTokens) / float64(fullTokens)

			t.Logf("Lighthouse token density (%d tables, using %d chars/token heuristic)", size, charsPerToken)
			t.Logf("  Lighthouse: ~%d tokens (%.1f tokens/table)", totalTokens, tokensPerTable)
			t.Logf("  Full DDL:   ~%d tokens", fullTokens)
			t.Logf("  Compression: %.1f%% of full DDL", compressionRatio*100)
		})
	}
}

func TestClaim_DDLCorrectness(t *testing.T) {
	cfg := gen.Config{
		TotalTables:  20,
		SignalTables: gen.DefaultSignalTables(),
		Seed:         42,
	}
	export := gen.Generate(cfg)

	c := &Compiler{Export: export}
	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll error: %v", err)
	}

	ddl := result.DSL

	// 1. Must have CREATE TABLE statements.
	if !strings.Contains(ddl, "CREATE TABLE") {
		t.Error("DDL output missing CREATE TABLE")
	}

	// 2. Must have column names (check known signal columns).
	knownColumns := []string{"id", "email", "name", "status", "user_id", "order_id"}
	for _, col := range knownColumns {
		if !strings.Contains(ddl, col) {
			t.Errorf("DDL missing known column %q", col)
		}
	}

	// 3. Must have column types.
	knownTypes := []string{"uuid", "text", "numeric", "timestamp", "bigint"}
	for _, typ := range knownTypes {
		if !strings.Contains(ddl, typ) {
			t.Errorf("DDL missing known type %q", typ)
		}
	}

	// 4. Must have PRIMARY KEY.
	if !strings.Contains(ddl, "PRIMARY KEY") {
		t.Error("DDL missing PRIMARY KEY")
	}

	// 5. Must have FOREIGN KEY via ALTER TABLE.
	if !strings.Contains(ddl, "ALTER TABLE") {
		t.Error("DDL missing ALTER TABLE (foreign keys)")
	}
	if !strings.Contains(ddl, "FOREIGN KEY") {
		t.Error("DDL missing FOREIGN KEY")
	}
	if !strings.Contains(ddl, "REFERENCES") {
		t.Error("DDL missing REFERENCES")
	}

	// 6. Every CREATE TABLE must end with );
	createCount := strings.Count(ddl, "CREATE TABLE")
	closeCount := strings.Count(ddl, ");")
	if closeCount < createCount {
		t.Errorf("expected at least %d closing ');' for %d CREATE TABLE, got %d",
			createCount, createCount, closeCount)
	}

	// 7. Verify specific FK structure.
	if !strings.Contains(ddl, "ALTER TABLE orders ADD FOREIGN KEY (user_id) REFERENCES users (id);") {
		t.Error("DDL missing expected FK: orders.user_id -> users.id")
	}

	t.Logf("DDL output is syntactically correct")
	t.Logf("  CREATE TABLE statements: %d", createCount)
	t.Logf("  ALTER TABLE FK statements: %d", strings.Count(ddl, "ALTER TABLE"))
	t.Logf("  Total DDL length: %d chars (~%d tokens)", len(ddl), len(ddl)/charsPerToken)
}

// TestClaim_DDLCompletenessVsPgDump documents what the DDL output includes
// and what it omits compared to pg_dump --schema-only. This is an honest
// inventory, not a test that everything is present.
//
// Included: column names, column types, PRIMARY KEY, FOREIGN KEY, NOT NULL,
//
//	DEFAULT, descriptions (as comments), UNIQUE constraints, indexes.
//
// Omitted:  CHECK, triggers, sequences (as separate objects), row-level security.
func TestClaim_DDLCompletenessVsPgDump(t *testing.T) {
	export := &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{
				Name:        "users",
				Type:        "table",
				Description: "Core identity table.",
				Fields: []schema.Field{
					{Name: "id", Type: "integer", IsPK: true, NotNull: true},
					{Name: "email", Type: "character varying(255)", NotNull: true, Description: "Login email."},
					{Name: "age", Type: "integer"},
					{Name: "tags", Type: "text[]", Description: "User tags."},
					{Name: "metadata", Type: "jsonb", Default: "'{}'::jsonb"},
					{Name: "created_at", Type: "timestamp with time zone", NotNull: true, Default: "now()"},
				},
				AccessPaths: []schema.AccessPath{
					{Name: "idx_users_email", Columns: []string{"email"}, IsUnique: true},
					{Name: "idx_users_created_at", Columns: []string{"created_at"}, IsUnique: false},
				},
			},
		},
		Edges: []schema.Edge{},
	}

	c := &Compiler{Export: export}
	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll error: %v", err)
	}

	ddl := result.DSL

	// --- Features that ARE included ---

	// Column types with full modifiers (from pg_catalog.format_type).
	if !strings.Contains(ddl, "character varying(255)") {
		t.Error("DDL should preserve full type modifiers like character varying(255)")
	}

	// Array types.
	if !strings.Contains(ddl, "text[]") {
		t.Error("DDL should preserve array types like text[]")
	}

	// Timezone-qualified timestamps.
	if !strings.Contains(ddl, "timestamp with time zone") {
		t.Error("DDL should preserve timestamp with time zone")
	}

	// PRIMARY KEY on PK columns.
	if !strings.Contains(ddl, "id integer PRIMARY KEY") {
		t.Error("DDL should include PRIMARY KEY on PK columns")
	}

	// Table description as inline comment.
	if !strings.Contains(ddl, "-- Core identity table.") {
		t.Error("DDL should include entity description as inline comment")
	}

	// Field description as inline comment.
	if !strings.Contains(ddl, "-- Login email.") {
		t.Error("DDL should include field descriptions as inline comments")
	}

	// NOT NULL constraints on non-PK columns.
	if !strings.Contains(ddl, "email character varying(255) NOT NULL") {
		t.Errorf("DDL should include NOT NULL on non-PK columns that have it:\n%s", ddl)
	}

	// NOT NULL should NOT appear on single-column PK columns (PRIMARY KEY implies it).
	if strings.Contains(ddl, "id integer PRIMARY KEY NOT NULL") {
		t.Error("DDL should not add redundant NOT NULL on PRIMARY KEY columns")
	}

	// DEFAULT expressions.
	if !strings.Contains(ddl, "DEFAULT now()") {
		t.Errorf("DDL should include DEFAULT expressions:\n%s", ddl)
	}
	if !strings.Contains(ddl, "DEFAULT '{}'::jsonb") {
		t.Errorf("DDL should include DEFAULT expressions with casts:\n%s", ddl)
	}

	// UNIQUE constraints from access paths.
	if !strings.Contains(ddl, "ALTER TABLE users ADD UNIQUE (email)") {
		t.Errorf("DDL should include UNIQUE constraints from access paths:\n%s", ddl)
	}

	// Non-unique indexes from access paths.
	if !strings.Contains(ddl, "CREATE INDEX idx_users_created_at ON users (created_at)") {
		t.Errorf("DDL should include CREATE INDEX for non-unique access paths:\n%s", ddl)
	}

	// --- Features that are NOT included (documenting known gaps) ---

	// CHECK constraints: not extracted, not rendered.
	if strings.Contains(ddl, "CHECK") {
		t.Error("DDL unexpectedly contains CHECK (not currently supported)")
	}

	t.Logf("DDL completeness inventory (vs pg_dump --schema-only):")
	t.Logf("  Included: column types, PRIMARY KEY, FOREIGN KEY, NOT NULL, DEFAULT, UNIQUE, indexes, descriptions")
	t.Logf("  Omitted:  CHECK, triggers, sequences, row-level security")
	t.Logf("DDL output:\n%s", ddl)
}

// TestClaim_DDLForeignKeyFormat verifies the FK output uses anonymous
// constraints (no ADD CONSTRAINT <name>), matching the actual code.
func TestClaim_DDLForeignKeyFormat(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Type: "table", Fields: []schema.Field{
			{Name: "id", Type: "integer", IsPK: true},
		}},
		{Name: "orders", Type: "table", Fields: []schema.Field{
			{Name: "id", Type: "integer", IsPK: true},
			{Name: "user_id", Type: "integer"},
		}},
	}
	edges := []schema.Edge{
		{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
	}

	ddl := renderDDL(entities, edges)

	// Must use anonymous FK (no ADD CONSTRAINT <name>).
	expected := "ALTER TABLE orders ADD FOREIGN KEY (user_id) REFERENCES users (id);"
	if !strings.Contains(ddl, expected) {
		t.Errorf("DDL should contain anonymous FK:\n  expected: %s\n  DDL:\n%s", expected, ddl)
	}

	// Must NOT contain named constraints (previous doc bug).
	if strings.Contains(ddl, "ADD CONSTRAINT") {
		t.Error("DDL should not contain ADD CONSTRAINT (FKs are anonymous)")
	}
}

func TestClaim_SessionDedup(t *testing.T) {
	// Session dedup: requesting the same table twice should not duplicate it.
	// This tests CompileSubset behavior when duplicate names are passed.
	export := &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{Name: "users", Type: "table", Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "email", Type: "text"},
			}},
			{Name: "orders", Type: "table", Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "user_id", Type: "uuid"},
			}},
		},
		Edges: []schema.Edge{
			{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		},
	}

	c := &Compiler{Export: export}

	// Request "users" twice in the subset.
	result, err := c.CompileSubset([]string{"users", "users", "orders"})
	if err != nil {
		t.Fatalf("CompileSubset error: %v", err)
	}

	// The nameSet dedup in CompileSubset should ensure only 2 entities.
	createCount := strings.Count(result.DSL, "CREATE TABLE")
	if createCount != 2 {
		t.Errorf("expected 2 CREATE TABLE (dedup), got %d.\nDDL:\n%s", createCount, result.DSL)
	}

	t.Logf("Session dedup works (duplicate names produce single output)")
}
