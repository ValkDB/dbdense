package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valkdb/dbdense/internal/gen"
	"github.com/valkdb/dbdense/pkg/schema"
)

func TestNewCompileCmdWritesCtxpack(t *testing.T) {
	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "ctxexport.json")
	outPath := filepath.Join(tmpDir, "ctxpack.txt")

	export := schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{
				Name: "users",
				Type: "table",
				Fields: []schema.Field{
					{Name: "id", Type: "bigint", IsPK: true},
					{Name: "email", Type: "text"},
				},
			},
		},
		Edges: []schema.Edge{},
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	if err := os.WriteFile(inPath, data, 0o644); err != nil {
		t.Fatalf("write ctxexport: %v", err)
	}

	cmd := newCompileCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--in", inPath, "--out", outPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute compile command: %v", err)
	}

	compiled, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read compiled output: %v", err)
	}
	output := string(compiled)
	if !strings.Contains(output, "-- dbdense schema context") {
		t.Fatalf("compiled output missing DDL header: %q", output)
	}
	if !strings.Contains(output, "CREATE TABLE users") {
		t.Fatalf("compiled output missing users entity: %q", output)
	}
}

func TestNewCompileCmdAcceptsGeneratedExport(t *testing.T) {
	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "ctxexport.json")
	outPath := filepath.Join(tmpDir, "ctxpack.txt")

	export := gen.Generate(gen.Config{
		TotalTables:  100,
		SignalTables: gen.DefaultSignalTables(),
		Seed:         100908,
	})
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	if err := os.WriteFile(inPath, data, 0o644); err != nil {
		t.Fatalf("write ctxexport: %v", err)
	}

	cmd := newCompileCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--in", inPath, "--out", outPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute compile command with generated export: %v", err)
	}

	compiled, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read compiled output: %v", err)
	}
	if !strings.Contains(string(compiled), "CREATE TABLE users") {
		t.Fatalf("compiled output missing generated DDL:\n%s", string(compiled))
	}
}

func TestSplitSchemas(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "multiple with whitespace", input: " public, auth ,billing ,, ", want: []string{"public", "auth", "billing"}},
		{name: "empty string returns nil", input: "", want: nil},
		{name: "single schema", input: "public", want: []string{"public"}},
		{name: "whitespace-padded single schema", input: "  public  ", want: []string{"public"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitSchemas(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitSchemas(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitSchemas(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeSplitBy(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"none":      "",
		"schema":    "schema",
		"namespace": "schema",
		"other":     "other",
	}
	for in, want := range cases {
		if got := normalizeSplitBy(in); got != want {
			t.Fatalf("normalizeSplitBy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNamespaceFileName(t *testing.T) {
	if got := namespaceFileName("auth"); got != "auth.ctxpack.txt" {
		t.Fatalf("unexpected namespace file: %q", got)
	}
	if got := namespaceFileName("Sales-OPS"); got != "sales_ops.ctxpack.txt" {
		t.Fatalf("unexpected namespace file: %q", got)
	}
	if got := namespaceFileName(""); got != "default.ctxpack.txt" {
		t.Fatalf("unexpected namespace file: %q", got)
	}
}

func TestCompileSplitBySchema_IncludesCrossSchemaEdges(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "ctxpacks")
	export := &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{
				Name: "users",
				Type: "table",
				Fields: []schema.Field{
					{Name: "id", Type: "bigint", IsPK: true},
					{Name: "session_id", Type: "uuid"},
				},
			},
			{
				Name: "auth.sessions",
				Type: "table",
				Fields: []schema.Field{
					{Name: "id", Type: "uuid", IsPK: true},
				},
			},
		},
		Edges: []schema.Edge{
			{FromEntity: "users", FromField: "session_id", ToEntity: "auth.sessions", ToField: "id"},
		},
	}

	if err := compileSplitBySchema(export, outDir); err != nil {
		t.Fatalf("compileSplitBySchema error: %v", err)
	}

	defaultPack, err := os.ReadFile(filepath.Join(outDir, "default.ctxpack.txt"))
	if err != nil {
		t.Fatalf("read default pack: %v", err)
	}
	authPack, err := os.ReadFile(filepath.Join(outDir, "auth.ctxpack.txt"))
	if err != nil {
		t.Fatalf("read auth pack: %v", err)
	}

	for _, pack := range []string{string(defaultPack), string(authPack)} {
		if !strings.Contains(pack, "includes cross-schema foreign keys") {
			t.Fatalf("expected cross-schema note in pack:\n%s", pack)
		}
		if !strings.Contains(pack, "ALTER TABLE users ADD FOREIGN KEY (session_id) REFERENCES auth.sessions (id);") {
			t.Fatalf("expected cross-schema FK in pack:\n%s", pack)
		}
	}
	if !strings.Contains(string(authPack), "CREATE TABLE auth.sessions (") {
		t.Fatalf("expected schema-qualified entity in auth pack:\n%s", string(authPack))
	}
}

func TestRunInitClaude_UsesAbsoluteExportPath(t *testing.T) {
	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir error: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	relativePath := filepath.Join("nested", "ctxexport.json")
	if err := runInitClaude(relativePath, "claude-code"); err != nil {
		t.Fatalf("runInitClaude error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}

	var cfg mcpConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	entry, ok := cfg.MCPServers["dbdense"]
	if !ok {
		t.Fatal("dbdense MCP entry missing")
	}
	if len(entry.Args) != 3 {
		t.Fatalf("unexpected args: %v", entry.Args)
	}
	wantPath := filepath.Join(tmpDir, "nested", "ctxexport.json")
	if entry.Args[2] != wantPath {
		t.Fatalf("config export path = %q, want %q", entry.Args[2], wantPath)
	}
}

func TestRunInitClaude_ReturnsInvalidJSONError(t *testing.T) {
	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir error: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	if err := os.WriteFile(".mcp.json", []byte("{"), 0o644); err != nil {
		t.Fatalf("write .mcp.json: %v", err)
	}

	err = runInitClaude("ctxexport.json", "claude-code")
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInitClaude_ReturnsReadError(t *testing.T) {
	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir error: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	if err := os.Mkdir(".mcp.json", 0o755); err != nil {
		t.Fatalf("mkdir .mcp.json: %v", err)
	}

	err = runInitClaude("ctxexport.json", "claude-code")
	if err == nil {
		t.Fatal("expected read error")
	}
	if !strings.Contains(err.Error(), "read config .mcp.json") {
		t.Fatalf("unexpected error: %v", err)
	}
}
