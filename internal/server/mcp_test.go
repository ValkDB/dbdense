package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/valkdb/dbdense/pkg/schema"
)

func testHandler() *handler {
	export := &schema.CtxExport{
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
			{Name: "audit_log", Type: "table", Fields: []schema.Field{
				{Name: "id", Type: "bigint", IsPK: true},
				{Name: "action", Type: "text"},
			}},
		},
		Edges: []schema.Edge{
			{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"},
		},
	}

	return &handler{
		export:   export,
		cachedLH: "# lighthouse.v0\nT:users|J:orders\nT:orders|J:users\nT:audit_log\n",
		sent:     make(map[string]bool, len(export.Entities)),
	}
}

// makeSliceRequest builds a CallToolRequest with the given table names.
func makeSliceRequest(tables ...string) mcp.CallToolRequest {
	tableArgs := make([]any, len(tables))
	for i, t := range tables {
		tableArgs[i] = t
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tables": tableArgs,
	}
	return req
}

// --- Resource handler tests ---

func TestHandleLighthouseResource(t *testing.T) {
	h := testHandler()
	contents, err := h.handleLighthouseResource(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 resource content, got %d", len(contents))
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatalf("expected TextResourceContents, got %T", contents[0])
	}
	if text.URI != "dbdense://lighthouse" {
		t.Errorf("URI = %q, want %q", text.URI, "dbdense://lighthouse")
	}
	if text.MIMEType != "text/plain" {
		t.Errorf("MIMEType = %q, want %q", text.MIMEType, "text/plain")
	}
	if text.Text != h.cachedLH {
		t.Errorf("Text mismatch:\ngot:  %q\nwant: %q", text.Text, h.cachedLH)
	}
}

// --- Slice tool tests ---

func TestHandleSliceTool_SpecificTables(t *testing.T) {
	h := testHandler()
	req := makeSliceRequest("users", "orders")

	result, err := h.handleSliceTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "CREATE TABLE users") {
		t.Error("expected users in slice result")
	}
	if !strings.Contains(text.Text, "CREATE TABLE orders") {
		t.Error("expected orders in slice result")
	}
	// audit_log was not requested.
	if strings.Contains(text.Text, "CREATE TABLE audit_log") {
		t.Error("audit_log should not appear when not requested")
	}
	// FK between orders->users should be included since both are in the subset.
	if !strings.Contains(text.Text, "FOREIGN KEY") {
		t.Error("expected FK between orders and users")
	}
}

func TestHandleSliceTool_AllEntities(t *testing.T) {
	h := testHandler()
	req := makeSliceRequest("users", "orders", "audit_log")

	result, err := h.handleSliceTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "CREATE TABLE users") {
		t.Error("expected users in slice result")
	}
	if !strings.Contains(text.Text, "CREATE TABLE orders") {
		t.Error("expected orders in slice result")
	}
	if !strings.Contains(text.Text, "CREATE TABLE audit_log") {
		t.Error("expected audit_log in slice result")
	}
}

func TestHandleSliceTool_SessionDedup(t *testing.T) {
	h := testHandler()

	// First call: request users and orders.
	req1 := makeSliceRequest("users", "orders")
	result1, err := h.handleSliceTool(context.Background(), req1)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if result1.IsError {
		t.Fatal("first call: expected non-error result")
	}
	text1, ok := result1.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result1.Content[0])
	}
	if !strings.Contains(text1.Text, "CREATE TABLE users") {
		t.Error("first call: expected users in result")
	}
	if !strings.Contains(text1.Text, "CREATE TABLE orders") {
		t.Error("first call: expected orders in result")
	}

	// Second call: request users (already sent) and audit_log (new).
	req2 := makeSliceRequest("users", "audit_log")
	result2, err := h.handleSliceTool(context.Background(), req2)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if result2.IsError {
		t.Fatal("second call: expected non-error result")
	}
	text2, ok := result2.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result2.Content[0])
	}
	// audit_log should appear (new table).
	if !strings.Contains(text2.Text, "CREATE TABLE audit_log") {
		t.Error("second call: expected audit_log in result")
	}
	// users should NOT appear in DDL (already sent).
	if strings.Contains(text2.Text, "CREATE TABLE users") {
		t.Error("second call: users should be deduped (already sent)")
	}
	// Should note that users was skipped.
	if !strings.Contains(text2.Text, "skipped") {
		t.Error("second call: expected skip note for users")
	}
	if !strings.Contains(text2.Text, "users") {
		t.Error("second call: skip note should mention users")
	}
}

func TestHandleSliceTool_AllAlreadySent(t *testing.T) {
	h := testHandler()

	// First call: send users and orders.
	req1 := makeSliceRequest("users", "orders")
	_, err := h.handleSliceTool(context.Background(), req1)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}

	// Second call: request the same tables.
	req2 := makeSliceRequest("users", "orders")
	result2, err := h.handleSliceTool(context.Background(), req2)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if result2.IsError {
		t.Fatal("second call: expected non-error result")
	}
	text2, ok := result2.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result2.Content[0])
	}
	if !strings.Contains(text2.Text, "already in your context") {
		t.Errorf("expected 'already in your context' message, got: %q", text2.Text)
	}
	if !strings.Contains(text2.Text, "users") || !strings.Contains(text2.Text, "orders") {
		t.Error("message should list the tables that were already sent")
	}
}

func TestHandleSliceTool_MissingTablesParam(t *testing.T) {
	h := testHandler()
	req := mcp.CallToolRequest{}

	result, err := h.handleSliceTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for missing tables param")
	}
}

func TestHandleSliceTool_EmptyTablesArray(t *testing.T) {
	h := testHandler()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tables": []any{},
	}

	result, err := h.handleSliceTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for empty tables array")
	}
}

func TestHandleSliceTool_NonexistentTables(t *testing.T) {
	h := testHandler()
	req := makeSliceRequest("nonexistent_table", "also_fake")

	result, err := h.handleSliceTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("nonexistent tables should not produce an error result")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	// DDL should have the header but no CREATE TABLE statements.
	if !strings.Contains(text.Text, "-- dbdense schema context") {
		t.Error("expected DDL header even for nonexistent tables")
	}
	if strings.Contains(text.Text, "CREATE TABLE") {
		t.Error("nonexistent tables should not produce CREATE TABLE statements")
	}
	// Should warn about tables not found.
	if !strings.Contains(text.Text, "Warning: tables not found in schema") {
		t.Error("expected warning about nonexistent tables")
	}
	if !strings.Contains(text.Text, "nonexistent_table") {
		t.Error("warning should mention nonexistent_table")
	}
	if !strings.Contains(text.Text, "also_fake") {
		t.Error("warning should mention also_fake")
	}
}

func TestHandleSliceTool_PartiallyNonexistent(t *testing.T) {
	h := testHandler()
	req := makeSliceRequest("users", "nonexistent_table")

	result, err := h.handleSliceTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	// users should appear in DDL.
	if !strings.Contains(text.Text, "CREATE TABLE users") {
		t.Error("expected users in partial result")
	}
	// Should warn about nonexistent_table.
	if !strings.Contains(text.Text, "Warning: tables not found in schema: nonexistent_table") {
		t.Error("expected warning about nonexistent_table")
	}
}

func TestHandleSliceTool_SessionDedupAllAlreadySentRepeated(t *testing.T) {
	h := testHandler()

	// Send all tables.
	req1 := makeSliceRequest("users", "orders", "audit_log")
	_, err := h.handleSliceTool(context.Background(), req1)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	// Request all again.
	req2 := makeSliceRequest("users", "orders", "audit_log")
	result2, err := h.handleSliceTool(context.Background(), req2)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	text2, ok := result2.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result2.Content[0])
	}
	if !strings.Contains(text2.Text, "already in your context") {
		t.Errorf("expected 'already in your context', got: %q", text2.Text)
	}
	// All three tables should be listed in the already-sent message.
	for _, name := range []string{"users", "orders", "audit_log"} {
		if !strings.Contains(text2.Text, name) {
			t.Errorf("already-sent message should mention %q", name)
		}
	}
}

func TestHandleSliceTool_SessionDedupMixedNewAndSent(t *testing.T) {
	h := testHandler()

	// First call: send only users.
	req1 := makeSliceRequest("users")
	result1, err := h.handleSliceTool(context.Background(), req1)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	text1, ok := result1.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result1.Content[0])
	}
	if !strings.Contains(text1.Text, "CREATE TABLE users") {
		t.Error("first call should include users DDL")
	}

	// Second call: request users (sent) and orders (new).
	req2 := makeSliceRequest("users", "orders")
	result2, err := h.handleSliceTool(context.Background(), req2)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	text2, ok := result2.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result2.Content[0])
	}
	// orders should appear as DDL (new).
	if !strings.Contains(text2.Text, "CREATE TABLE orders") {
		t.Error("second call should include orders DDL")
	}
	// users should NOT appear as DDL (already sent).
	if strings.Contains(text2.Text, "CREATE TABLE users") {
		t.Error("second call should not re-send users DDL")
	}
	// Skip note should mention users.
	if !strings.Contains(text2.Text, "skipped") {
		t.Error("second call should have a skip note")
	}
	if !strings.Contains(text2.Text, "users") {
		t.Error("skip note should mention users")
	}
}

func TestHandleSliceTool_LargeTableNameList(t *testing.T) {
	h := testHandler()

	// Build a request with many table names (mostly nonexistent).
	largeCount := 100
	tables := make([]any, largeCount)
	tables[0] = "users"
	tables[1] = "orders"
	for i := 2; i < largeCount; i++ {
		tables[i] = fmt.Sprintf("nonexistent_table_%d", i)
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tables": tables,
	}

	result, err := h.handleSliceTool(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("large table list should not produce error")
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	// Should contain the real tables.
	if !strings.Contains(text.Text, "CREATE TABLE users") {
		t.Error("expected users in result")
	}
	if !strings.Contains(text.Text, "CREATE TABLE orders") {
		t.Error("expected orders in result")
	}
}

// --- Reset tool tests ---

func TestHandleResetTool_ClearsSession(t *testing.T) {
	h := testHandler()

	// First: send some tables so the session cache is populated.
	req1 := makeSliceRequest("users", "orders")
	_, err := h.handleSliceTool(context.Background(), req1)
	if err != nil {
		t.Fatalf("first slice call: unexpected error: %v", err)
	}

	// Verify dedup is active: requesting the same tables gives "already in context".
	req2 := makeSliceRequest("users", "orders")
	result2, err := h.handleSliceTool(context.Background(), req2)
	if err != nil {
		t.Fatalf("second slice call: unexpected error: %v", err)
	}
	text2, ok := result2.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result2.Content[0])
	}
	if !strings.Contains(text2.Text, "already in your context") {
		t.Errorf("expected 'already in your context' before reset, got: %q", text2.Text)
	}

	// Reset the session cache.
	resetResult, err := h.handleResetTool(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("reset call: unexpected error: %v", err)
	}
	if resetResult.IsError {
		t.Fatal("reset call: expected non-error result")
	}
	resetText, ok := resetResult.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", resetResult.Content[0])
	}
	if !strings.Contains(resetText.Text, "Session cache cleared") {
		t.Errorf("expected 'Session cache cleared' message, got: %q", resetText.Text)
	}

	// After reset: requesting the same tables should return DDL again (not "already in context").
	req3 := makeSliceRequest("users", "orders")
	result3, err := h.handleSliceTool(context.Background(), req3)
	if err != nil {
		t.Fatalf("post-reset slice call: unexpected error: %v", err)
	}
	if result3.IsError {
		t.Fatal("post-reset slice call: expected non-error result")
	}
	text3, ok := result3.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result3.Content[0])
	}
	if !strings.Contains(text3.Text, "CREATE TABLE users") {
		t.Error("post-reset: expected users DDL to be re-sent")
	}
	if !strings.Contains(text3.Text, "CREATE TABLE orders") {
		t.Error("post-reset: expected orders DDL to be re-sent")
	}
	if strings.Contains(text3.Text, "already in your context") {
		t.Error("post-reset: should not say 'already in your context'")
	}
}

func TestParseStringSlice(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    []string
		wantErr bool
	}{
		{
			name:  "valid string array",
			input: []any{"users", "orders"},
			want:  []string{"users", "orders"},
		},
		{
			name:    "not an array",
			input:   "users",
			wantErr: true,
		},
		{
			name:    "array with non-string element",
			input:   []any{"users", 42},
			wantErr: true,
		},
		{
			name:  "empty array",
			input: []any{},
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStringSlice(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseStringSlice() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(got) != len(tt.want) {
				t.Errorf("parseStringSlice() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseStringSlice()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestHandleSliceTool_NonexistentNotMarkedAsSent verifies that nonexistent
// tables are NOT marked as sent. A subsequent request for the same fake table
// should not claim it is "already in your context".
func TestHandleSliceTool_NonexistentNotMarkedAsSent(t *testing.T) {
	h := testHandler()

	// First call: request a mix of real and fake tables.
	req1 := makeSliceRequest("users", "fake_table")
	result1, err := h.handleSliceTool(context.Background(), req1)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	text1, ok := result1.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result1.Content[0])
	}
	if !strings.Contains(text1.Text, "CREATE TABLE users") {
		t.Error("first call should include users DDL")
	}
	if !strings.Contains(text1.Text, "Warning: tables not found in schema: fake_table") {
		t.Error("first call should warn about fake_table")
	}

	// Second call: request fake_table again. It should NOT be reported as
	// "already in your context" because it was never actually sent.
	req2 := makeSliceRequest("fake_table")
	result2, err := h.handleSliceTool(context.Background(), req2)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	text2, ok := result2.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result2.Content[0])
	}
	if strings.Contains(text2.Text, "already in your context") {
		t.Error("fake_table should NOT be marked as already sent")
	}
	if !strings.Contains(text2.Text, "Warning: tables not found in schema: fake_table") {
		t.Error("second call should still warn about fake_table")
	}
}

// TestHandleSliceTool_ConcurrentAccess verifies that concurrent slice calls
// do not race on the handler's sent map. This test uses the -race detector;
// a data race will cause it to fail.
func TestHandleSliceTool_ConcurrentAccess(t *testing.T) {
	h := testHandler()

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			// Alternate between different table sets to create contention.
			var req mcp.CallToolRequest
			if idx%2 == 0 {
				req = makeSliceRequest("users", "orders")
			} else {
				req = makeSliceRequest("audit_log", "users")
			}
			result, err := h.handleSliceTool(context.Background(), req)
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
				return
			}
			if result == nil {
				t.Errorf("goroutine %d: nil result", idx)
			}
		}(i)
	}

	wg.Wait()
}
