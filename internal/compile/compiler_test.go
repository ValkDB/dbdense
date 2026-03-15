package compile

import (
	"bytes"
	"strings"
	"testing"

	"github.com/valkdb/dbdense/pkg/schema"
)

// testExport returns a small CtxExport fixture used across compiler tests.
func testExport() *schema.CtxExport {
	return &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{Name: "users", Type: "table", Description: "Core identity table.", Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "email", Type: "text", Description: "Login email."},
				{Name: "deleted_at", Type: "timestamp", Description: "Soft delete flag."},
			}},
			{Name: "orders", Type: "table", Description: "Customer orders.", Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "user_id", Type: "uuid", Description: "FK to users."},
				{Name: "status", Type: "text", Description: "Order status."},
			}},
			{Name: "products", Type: "table", Description: "Product catalog.", Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "name", Type: "text"},
			}},
			{Name: "audit_log", Type: "table", Description: "System audit trail.", Fields: []schema.Field{
				{Name: "id", Type: "bigint", IsPK: true},
				{Name: "action", Type: "text"},
			}},
		},
		Edges: []schema.Edge{
			{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		},
	}
}

func TestCompileAll(t *testing.T) {
	c := &Compiler{Export: testExport()}
	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll error: %v", err)
	}

	if len(result.Entities) != 4 {
		t.Errorf("CompileAll: expected 4 entities, got %d", len(result.Entities))
	}
	if len(result.Edges) != 1 {
		t.Errorf("CompileAll: expected 1 edge, got %d", len(result.Edges))
	}
	if !strings.HasPrefix(result.DSL, "-- dbdense schema context\n") {
		t.Error("DSL should start with DDL header")
	}
	if !strings.Contains(result.DSL, "CREATE TABLE") {
		t.Error("DSL should contain CREATE TABLE statements")
	}
}

func TestCompileAll_NilExport(t *testing.T) {
	c := &Compiler{Export: nil}
	_, err := c.CompileAll()
	if err == nil {
		t.Error("expected error for nil export")
	}
}

func TestRenderDDL_Format(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Description: "Identity.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "email", Type: "text", Description: "Login email."},
		}},
	}
	edges := []schema.Edge{
		{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"},
	}

	ddl := renderDDL(entities, edges)

	if !strings.Contains(ddl, "CREATE TABLE users (") {
		t.Errorf("expected CREATE TABLE users in DDL:\n%s", ddl)
	}
	if !strings.Contains(ddl, "id uuid PRIMARY KEY") {
		t.Errorf("expected id uuid PRIMARY KEY in DDL:\n%s", ddl)
	}
	if !strings.Contains(ddl, "email text") {
		t.Errorf("expected email text in DDL:\n%s", ddl)
	}
	if !strings.Contains(ddl, "-- Login email.") {
		t.Errorf("expected field description comment in DDL:\n%s", ddl)
	}
	if !strings.Contains(ddl, "-- Identity.") {
		t.Errorf("expected entity description comment in DDL:\n%s", ddl)
	}
	if !strings.Contains(ddl, "ALTER TABLE orders ADD FOREIGN KEY (user_id) REFERENCES users (id);") {
		t.Errorf("expected ALTER TABLE FK in DDL:\n%s", ddl)
	}
}

func TestRenderDDL_CompositePK(t *testing.T) {
	entities := []schema.Entity{
		{Name: "order_items", Fields: []schema.Field{
			{Name: "order_id", Type: "uuid", IsPK: true},
			{Name: "product_id", Type: "uuid", IsPK: true},
			{Name: "quantity", Type: "int"},
		}},
	}
	ddl := renderDDL(entities, nil)
	if !strings.Contains(ddl, "PRIMARY KEY (order_id, product_id)") {
		t.Errorf("expected composite PK in DDL, got:\n%s", ddl)
	}
	// Individual PK columns should NOT have PRIMARY KEY suffix.
	if strings.Contains(ddl, "order_id uuid PRIMARY KEY") {
		t.Errorf("composite PK columns should not have inline PRIMARY KEY:\n%s", ddl)
	}
}

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple", in: "users", want: "users"},
		{name: "underscore", in: "order_items", want: "order_items"},
		{name: "reserved word", in: "select", want: `"select"`},
		{name: "starts with digit", in: "123users", want: `"123users"`},
		{name: "mixed case", in: "Auth", want: `"Auth"`},
		{name: "hyphen", in: "my-table", want: `"my-table"`},
		{name: "space", in: "user name", want: `"user name"`},
		{name: "dot", in: "auth.sessions", want: `"auth.sessions"`},
		{name: "embedded quote", in: `say"hi`, want: `"say""hi"`},
	}

	for _, tt := range tests {
		if got := quoteIdentifier(tt.in); got != tt.want {
			t.Errorf("%s: quoteIdentifier(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestQuoteQualifiedName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple", in: "users", want: "users"},
		{name: "schema qualified", in: "auth.sessions", want: "auth.sessions"},
		{name: "quoted table", in: "auth.user sessions", want: `auth."user sessions"`},
		{name: "quoted schema and table", in: "Auth.select", want: `"Auth"."select"`},
		{name: "multiple dots stay single identifier", in: "table.with.dots", want: `"table.with.dots"`},
	}

	for _, tt := range tests {
		if got := quoteQualifiedName(tt.in); got != tt.want {
			t.Errorf("%s: quoteQualifiedName(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestRenderDDL_QuotesIdentifiers(t *testing.T) {
	entities := []schema.Entity{
		{Name: "auth.sessions", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user name", Type: "text"},
			{Name: "from", Type: "text"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx auth sessions user", Columns: []string{"user name"}, IsUnique: false},
		}},
		{Name: "select", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "order", Type: "text"},
		}},
	}
	edges := []schema.Edge{
		{FromEntity: "auth.sessions", FromField: "from", ToEntity: "select", ToField: "id"},
	}

	ddl := renderDDL(entities, edges)

	if !strings.Contains(ddl, `CREATE TABLE auth.sessions (`) {
		t.Fatalf("expected quoted schema-like table name, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `  "user name" text`) {
		t.Fatalf("expected quoted field with space, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `  "from" text`) {
		t.Fatalf("expected quoted reserved-word field, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `CREATE TABLE "select" (`) {
		t.Fatalf("expected quoted reserved-word entity, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `CREATE INDEX "idx auth sessions user" ON auth.sessions ("user name");`) {
		t.Fatalf("expected quoted index statement, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `ALTER TABLE auth.sessions ADD FOREIGN KEY ("from") REFERENCES "select" (id);`) {
		t.Fatalf("expected quoted FK statement, got:\n%s", ddl)
	}
}

func TestSanitizeLighthouse(t *testing.T) {
	input := "user|name=test(foo),bar>baz\n#line2:ok\rend"
	got := sanitizeLighthouse(input)
	for _, c := range []string{"|", "=", "(", ")", ">", "\n", ":", "#", "\r"} {
		if strings.Contains(got, c) {
			t.Errorf("sanitizeLighthouse(%q) = %q, contains delimiter %q", input, got, c)
		}
	}
	// Comma is replaced with space, not stripped.
	if !strings.Contains(got, " ") {
		t.Errorf("sanitizeLighthouse(%q) = %q, comma should be replaced with space", input, got)
	}
}

func TestSanitizeLighthouse_ColonAndCR(t *testing.T) {
	// A table named "T:injected" would produce ambiguous lighthouse line
	// "T:T:injected" if colon is not stripped.
	got := sanitizeLighthouse("T:injected")
	if strings.Contains(got, ":") {
		t.Errorf("sanitizeLighthouse should strip colons, got %q", got)
	}
	if got != "Tinjected" {
		t.Errorf("sanitizeLighthouse(\"T:injected\") = %q, want \"Tinjected\"", got)
	}

	// A table with \r could inject line breaks on Windows.
	got = sanitizeLighthouse("bad\rname")
	if strings.Contains(got, "\r") {
		t.Errorf("sanitizeLighthouse should strip \\r, got %q", got)
	}
	if got != "badname" {
		t.Errorf("sanitizeLighthouse(\"bad\\rname\") = %q, want \"badname\"", got)
	}

	// A leading # would be parsed as a comment line in lighthouse format.
	got = sanitizeLighthouse("#comment")
	if strings.Contains(got, "#") {
		t.Errorf("sanitizeLighthouse should strip #, got %q", got)
	}
	if got != "comment" {
		t.Errorf("sanitizeLighthouse(\"#comment\") = %q, want \"comment\"", got)
	}
}

func TestRenderDDL_Header(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
		}},
	}
	ddl := renderDDL(entities, nil)

	lines := strings.SplitN(ddl, "\n", 3)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}
	if lines[0] != "-- dbdense schema context" {
		t.Errorf("first line should be DDL header, got %q", lines[0])
	}
}

func TestRenderDDL_Subfields(t *testing.T) {
	entities := []schema.Entity{
		{Name: "orders", Fields: []schema.Field{
			{Name: "_id", Type: "objectId", IsPK: true},
			{Name: "payload", Type: "object", Subfields: []schema.Field{
				{Name: "channel", Type: "string"},
				{Name: "status", Type: "string"},
			}},
			{Name: "status", Type: "string"},
		}},
	}
	ddl := renderDDL(entities, nil)

	// Subfields are not rendered in DDL mode; the column itself appears.
	if !strings.Contains(ddl, "payload object") {
		t.Errorf("expected payload column in DDL, got:\n%s", ddl)
	}
}

func TestRenderDDL_SubfieldsWithDescription(t *testing.T) {
	entities := []schema.Entity{
		{Name: "orders", Fields: []schema.Field{
			{Name: "_id", Type: "objectId", IsPK: true},
			{Name: "payload", Type: "object", Description: "Order payload.", Subfields: []schema.Field{
				{Name: "status", Type: "string"},
			}},
		}},
	}
	ddl := renderDDL(entities, nil)

	if !strings.Contains(ddl, "-- Order payload.") {
		t.Errorf("expected description comment in DDL, got:\n%s", ddl)
	}
}

func TestSanitizeLighthouse_RoundTrip(t *testing.T) {
	// Clean input should pass through unchanged.
	if got := sanitizeLighthouse("users"); got != "users" {
		t.Errorf("sanitizeLighthouse(\"users\") = %q, want \"users\"", got)
	}
}

func TestWriteDDL(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "email", Type: "text"},
		}},
	}
	edges := []schema.Edge{
		{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"},
	}

	var buf bytes.Buffer
	if err := WriteDDL(&buf, entities, edges); err != nil {
		t.Fatalf("WriteDDL error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("WriteDDL produced empty output")
	}
	if !strings.HasPrefix(buf.String(), "-- dbdense schema context\n") {
		t.Error("WriteDDL output should start with DDL header")
	}
}

func TestWriteLighthouse(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
		}},
	}
	edges := []schema.Edge{
		{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"},
	}

	var buf bytes.Buffer
	if err := WriteLighthouse(&buf, entities, edges); err != nil {
		t.Fatalf("WriteLighthouse error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("WriteLighthouse produced empty output")
	}
	if !strings.HasPrefix(buf.String(), "# lighthouse.v0\n") {
		t.Error("WriteLighthouse output should start with lighthouse.v0 header")
	}
}

func TestRenderDDL_ViewEntity(t *testing.T) {
	entities := []schema.Entity{
		{Name: "active_users", Type: "view", Fields: []schema.Field{
			{Name: "id", Type: "uuid"},
			{Name: "email", Type: "text"},
		}},
	}
	ddl := renderDDL(entities, nil)
	if !strings.Contains(ddl, "-- VIEW: active_users (id, email)") {
		t.Errorf("expected VIEW comment for view entity, got:\n%s", ddl)
	}
	if strings.Contains(ddl, "CREATE TABLE active_users") {
		t.Errorf("view entities should not use CREATE TABLE:\n%s", ddl)
	}
}

func TestRenderDDL_EmptyType(t *testing.T) {
	entities := []schema.Entity{
		{Name: "events", Fields: []schema.Field{
			{Name: "id", IsPK: true},
			{Name: "data"},
		}},
	}
	ddl := renderDDL(entities, nil)
	// Column with no type should just show the name.
	if !strings.Contains(ddl, "  id PRIMARY KEY") {
		t.Errorf("expected 'id PRIMARY KEY' without type, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "  data\n") {
		t.Errorf("expected 'data' without type, got:\n%s", ddl)
	}
}

func TestCompileSubset(t *testing.T) {
	c := &Compiler{Export: testExport()}
	result, err := c.CompileSubset([]string{"users", "orders"})
	if err != nil {
		t.Fatalf("CompileSubset error: %v", err)
	}

	if len(result.Entities) != 2 {
		t.Errorf("CompileSubset: expected 2 entities, got %d", len(result.Entities))
	}
	if len(result.Edges) != 1 {
		t.Errorf("CompileSubset: expected 1 edge (orders->users), got %d", len(result.Edges))
	}
	if !strings.Contains(result.DSL, "CREATE TABLE users") {
		t.Error("subset should contain users")
	}
	if !strings.Contains(result.DSL, "CREATE TABLE orders") {
		t.Error("subset should contain orders")
	}
	if strings.Contains(result.DSL, "CREATE TABLE products") {
		t.Error("subset should NOT contain products")
	}
}

func TestCompileSubset_EdgesFilteredByBothEndpoints(t *testing.T) {
	c := &Compiler{Export: testExport()}
	// Only include "orders" but not "users" — the edge should be excluded.
	result, err := c.CompileSubset([]string{"orders"})
	if err != nil {
		t.Fatalf("CompileSubset error: %v", err)
	}

	if len(result.Entities) != 1 {
		t.Errorf("expected 1 entity, got %d", len(result.Entities))
	}
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges (users not in subset), got %d", len(result.Edges))
	}
}

func TestCompileSubset_NilExport(t *testing.T) {
	c := &Compiler{Export: nil}
	_, err := c.CompileSubset([]string{"users"})
	if err == nil {
		t.Error("expected error for nil export")
	}
}

func TestRenderDDL_NoEdges(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
		}},
	}
	ddl := renderDDL(entities, nil)
	if strings.Contains(ddl, "Foreign keys") {
		t.Errorf("DDL with no edges should not contain Foreign keys section:\n%s", ddl)
	}
}

func TestCompileSubset_EmptyNamesList(t *testing.T) {
	c := &Compiler{Export: testExport()}
	result, err := c.CompileSubset([]string{})
	if err != nil {
		t.Fatalf("CompileSubset error: %v", err)
	}
	if len(result.Entities) != 0 {
		t.Errorf("expected 0 entities for empty names, got %d", len(result.Entities))
	}
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges for empty names, got %d", len(result.Edges))
	}
	// DSL should contain only the header line.
	if !strings.HasPrefix(result.DSL, "-- dbdense schema context\n") {
		t.Errorf("empty subset DSL should still have header, got: %q", result.DSL)
	}
	if strings.Contains(result.DSL, "CREATE TABLE") {
		t.Error("empty subset DSL should not contain CREATE TABLE")
	}
}

func TestCompileSubset_NoMatchingNames(t *testing.T) {
	c := &Compiler{Export: testExport()}
	result, err := c.CompileSubset([]string{"nonexistent", "also_fake"})
	if err != nil {
		t.Fatalf("CompileSubset error: %v", err)
	}
	if len(result.Entities) != 0 {
		t.Errorf("expected 0 entities for nonexistent names, got %d", len(result.Entities))
	}
	if len(result.Edges) != 0 {
		t.Errorf("expected 0 edges for nonexistent names, got %d", len(result.Edges))
	}
	if strings.Contains(result.DSL, "CREATE TABLE") {
		t.Error("nonexistent names should not produce CREATE TABLE in DSL")
	}
}

func TestCompileSubset_NilNamesList(t *testing.T) {
	c := &Compiler{Export: testExport()}
	result, err := c.CompileSubset(nil)
	if err != nil {
		t.Fatalf("CompileSubset error: %v", err)
	}
	if len(result.Entities) != 0 {
		t.Errorf("expected 0 entities for nil names, got %d", len(result.Entities))
	}
}

func TestRenderDDL_MultipleFieldDescriptions(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Description: "Core identity.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "email", Type: "text", Description: "Login email."},
			{Name: "deleted_at", Type: "timestamp", Description: "Soft delete flag."},
		}},
	}
	ddl := renderDDL(entities, nil)
	if !strings.Contains(ddl, "-- Login email.") {
		t.Errorf("expected Login email description, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "-- Soft delete flag.") {
		t.Errorf("expected Soft delete flag description, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "-- Core identity.") {
		t.Errorf("expected entity description, got:\n%s", ddl)
	}
}

func TestRenderDDL_ViewWithDescription(t *testing.T) {
	entities := []schema.Entity{
		{Name: "active_users", Type: "view", Description: "Only non-deleted users.", Fields: []schema.Field{
			{Name: "id", Type: "uuid"},
			{Name: "email", Type: "text"},
		}},
	}
	ddl := renderDDL(entities, nil)
	if !strings.Contains(ddl, "-- VIEW: active_users (id, email)") {
		t.Errorf("expected VIEW comment, got:\n%s", ddl)
	}
	// Views should not produce CREATE TABLE.
	if strings.Contains(ddl, "CREATE TABLE") {
		t.Errorf("view should not produce CREATE TABLE:\n%s", ddl)
	}
}

func TestRenderDDL_MixedTableAndView(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Type: "table", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "email", Type: "text"},
		}},
		{Name: "active_users", Type: "view", Fields: []schema.Field{
			{Name: "id", Type: "uuid"},
			{Name: "email", Type: "text"},
		}},
	}
	ddl := renderDDL(entities, nil)
	if !strings.Contains(ddl, "CREATE TABLE users") {
		t.Errorf("expected CREATE TABLE users:\n%s", ddl)
	}
	if !strings.Contains(ddl, "-- VIEW: active_users") {
		t.Errorf("expected VIEW comment for active_users:\n%s", ddl)
	}
	if strings.Contains(ddl, "CREATE TABLE active_users") {
		t.Errorf("view should not produce CREATE TABLE:\n%s", ddl)
	}
}

func TestRenderDDL_NotNull(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true, NotNull: true},
			{Name: "email", Type: "text", NotNull: true},
			{Name: "bio", Type: "text"},
		}},
	}
	ddl := renderDDL(entities, nil)

	// PK column should NOT have redundant NOT NULL.
	if strings.Contains(ddl, "PRIMARY KEY NOT NULL") {
		t.Errorf("PRIMARY KEY should not have redundant NOT NULL:\n%s", ddl)
	}

	// Non-PK NOT NULL column should have NOT NULL.
	if !strings.Contains(ddl, "email text NOT NULL") {
		t.Errorf("expected NOT NULL on email column:\n%s", ddl)
	}

	// Nullable column should NOT have NOT NULL.
	if strings.Contains(ddl, "bio text NOT NULL") {
		t.Errorf("nullable column should not have NOT NULL:\n%s", ddl)
	}
}

func TestRenderDDL_NotNull_CompositePK(t *testing.T) {
	// With composite PK, individual columns don't get PRIMARY KEY inline,
	// so NOT NULL should render on PK fields.
	entities := []schema.Entity{
		{Name: "order_items", Fields: []schema.Field{
			{Name: "order_id", Type: "uuid", IsPK: true, NotNull: true},
			{Name: "product_id", Type: "uuid", IsPK: true, NotNull: true},
			{Name: "quantity", Type: "int", NotNull: true},
		}},
	}
	ddl := renderDDL(entities, nil)

	if !strings.Contains(ddl, "order_id uuid NOT NULL") {
		t.Errorf("composite PK column should show NOT NULL:\n%s", ddl)
	}
	if !strings.Contains(ddl, "product_id uuid NOT NULL") {
		t.Errorf("composite PK column should show NOT NULL:\n%s", ddl)
	}
	if !strings.Contains(ddl, "quantity int NOT NULL") {
		t.Errorf("regular NOT NULL column should show NOT NULL:\n%s", ddl)
	}
}

func TestRenderDDL_Default(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "status", Type: "text", NotNull: true, Default: "'active'::text"},
			{Name: "created_at", Type: "timestamp", Default: "now()"},
			{Name: "bio", Type: "text"},
		}},
	}
	ddl := renderDDL(entities, nil)

	// NOT NULL before DEFAULT. "status" is a SQL reserved word, so it gets quoted.
	if !strings.Contains(ddl, `"status" text NOT NULL DEFAULT 'active'::text`) {
		t.Errorf("expected NOT NULL DEFAULT on status:\n%s", ddl)
	}

	// DEFAULT without NOT NULL.
	if !strings.Contains(ddl, "created_at timestamp DEFAULT now()") {
		t.Errorf("expected DEFAULT on created_at:\n%s", ddl)
	}

	// No DEFAULT on bio.
	if strings.Contains(ddl, "bio text DEFAULT") {
		t.Errorf("bio should not have DEFAULT:\n%s", ddl)
	}
}

func TestRenderDDL_AccessPaths_Unique(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "email", Type: "text"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_users_email", Columns: []string{"email"}, IsUnique: true},
		}},
	}
	ddl := renderDDL(entities, nil)

	if !strings.Contains(ddl, "ALTER TABLE users ADD UNIQUE (email);") {
		t.Errorf("expected UNIQUE constraint for unique access path:\n%s", ddl)
	}
}

func TestRenderDDL_AccessPaths_NonUnique(t *testing.T) {
	entities := []schema.Entity{
		{Name: "orders", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "created_at", Type: "timestamp"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_orders_created_at", Columns: []string{"created_at"}, IsUnique: false},
		}},
	}
	ddl := renderDDL(entities, nil)

	if !strings.Contains(ddl, "CREATE INDEX idx_orders_created_at ON orders (created_at);") {
		t.Errorf("expected CREATE INDEX for non-unique access path:\n%s", ddl)
	}
}

func TestRenderDDL_AccessPaths_MultiColumn(t *testing.T) {
	entities := []schema.Entity{
		{Name: "events", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid"},
			{Name: "created_at", Type: "timestamp"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_events_user_created", Columns: []string{"user_id", "created_at"}, IsUnique: false},
		}},
	}
	ddl := renderDDL(entities, nil)

	if !strings.Contains(ddl, "CREATE INDEX idx_events_user_created ON events (user_id, created_at);") {
		t.Errorf("expected multi-column CREATE INDEX:\n%s", ddl)
	}
}

func TestRenderDDL_AccessPaths_MultiColumnUnique(t *testing.T) {
	entities := []schema.Entity{
		{Name: "memberships", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid"},
			{Name: "group_id", Type: "uuid"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_memberships_user_group", Columns: []string{"user_id", "group_id"}, IsUnique: true},
		}},
	}
	ddl := renderDDL(entities, nil)

	if !strings.Contains(ddl, "ALTER TABLE memberships ADD UNIQUE (user_id, group_id);") {
		t.Errorf("expected multi-column UNIQUE constraint:\n%s", ddl)
	}
}

func TestRenderDDL_AccessPaths_NoAccessPaths(t *testing.T) {
	entities := []schema.Entity{
		{Name: "users", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "email", Type: "text"},
		}},
	}
	ddl := renderDDL(entities, nil)

	if strings.Contains(ddl, "CREATE INDEX") {
		t.Errorf("entity with no access paths should not have CREATE INDEX:\n%s", ddl)
	}
	if strings.Contains(ddl, "ADD UNIQUE") {
		t.Errorf("entity with no access paths should not have ADD UNIQUE:\n%s", ddl)
	}
}

func FuzzSanitizeLighthouse(f *testing.F) {
	f.Add("users")
	f.Add("auth.sessions")
	f.Add("my-table")
	f.Add("#comment")
	f.Add("T:injected|J:fake")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		result := sanitizeLighthouse(s)
		// Result must not contain any structural delimiters.
		for _, c := range []string{"|", "=", "(", ")", ">", "\n", ":", "#", "\r"} {
			if strings.Contains(result, c) {
				t.Errorf("sanitizeLighthouse(%q) = %q, contains delimiter %q", s, result, c)
			}
		}
	})
}

func TestRenderDDL_ViewIgnoresAccessPaths(t *testing.T) {
	// Views are rendered as comments, so access paths should not appear.
	entities := []schema.Entity{
		{Name: "active_users", Type: "view", Fields: []schema.Field{
			{Name: "id", Type: "uuid"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_active_users", Columns: []string{"id"}, IsUnique: true},
		}},
	}
	ddl := renderDDL(entities, nil)

	if strings.Contains(ddl, "ADD UNIQUE") {
		t.Errorf("view should not emit access path statements:\n%s", ddl)
	}
	if strings.Contains(ddl, "CREATE INDEX") {
		t.Errorf("view should not emit index statements:\n%s", ddl)
	}
}
