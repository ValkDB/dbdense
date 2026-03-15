package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valkdb/dbdense/pkg/schema"
)

func TestLoadSidecar_NotFound(t *testing.T) {
	sc, err := LoadSidecar("/nonexistent/path/dbdense.yaml")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if sc != nil {
		t.Fatal("expected nil sidecar for missing file")
	}
}

func TestLoadSidecar_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dbdense.yaml")

	content := `entities:
  users:
    description: "Core identity table."
    fields:
      deleted_at:
        description: "Soft delete flag."
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sc, err := LoadSidecar(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc == nil {
		t.Fatal("expected non-nil sidecar")
	}
	if sc.Entities["users"].Description != "Core identity table." {
		t.Errorf("unexpected entity description: %q", sc.Entities["users"].Description)
	}
	if sc.Entities["users"].Fields["deleted_at"].Description != "Soft delete flag." {
		t.Errorf("unexpected field description: %q", sc.Entities["users"].Fields["deleted_at"].Description)
	}
}

func TestLoadSidecar_UnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dbdense.yaml")

	content := `entities:
  users:
    description: "desc"
    unknown_key: "should fail"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSidecar(path)
	if err == nil {
		t.Error("expected error for unknown fields in strict mode")
	}
}

func TestMergeSidecar(t *testing.T) {
	export := &schema.CtxExport{
		Entities: []schema.Entity{
			{Name: "users", Description: "original desc", Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "deleted_at", Type: "timestamp", Description: ""},
			}},
			{Name: "orders", Description: "order desc", Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
			}},
		},
	}

	sc := &Sidecar{
		Entities: map[string]SidecarEntity{
			"users": {
				Description: "Overridden description.",
				Fields: map[string]SidecarField{
					"deleted_at": {Description: "Soft delete flag."},
				},
			},
		},
	}

	warnings := MergeSidecar(export, sc)

	if export.Entities[0].Description != "Overridden description." {
		t.Errorf("entity description not merged: %q", export.Entities[0].Description)
	}
	if export.Entities[0].Fields[1].Description != "Soft delete flag." {
		t.Errorf("field description not merged: %q", export.Entities[0].Fields[1].Description)
	}
	// orders should be unchanged.
	if export.Entities[1].Description != "order desc" {
		t.Errorf("unrelated entity was modified: %q", export.Entities[1].Description)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestMergeSidecar_NilSidecar(t *testing.T) {
	export := &schema.CtxExport{
		Entities: []schema.Entity{
			{Name: "users", Description: "original"},
		},
	}
	warnings := MergeSidecar(export, nil)
	if export.Entities[0].Description != "original" {
		t.Error("nil sidecar should not modify export")
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestMergeSidecar_NilExport(t *testing.T) {
	sc := &Sidecar{
		Entities: map[string]SidecarEntity{
			"users": {Description: "test"},
		},
	}
	warnings := MergeSidecar(nil, sc)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for nil export, got %v", warnings)
	}
}

func TestLoadSidecar_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dbdense.yaml")

	if err := os.WriteFile(path, []byte("{{{not yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSidecar(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadSidecar_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dbdense.yaml")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// Empty file causes yaml.NewDecoder.Decode to return io.EOF, which
	// LoadSidecar wraps as "parse sidecar: EOF". Assert the error.
	_, err := LoadSidecar(path)
	if err == nil {
		t.Fatal("expected error for empty YAML file, got nil")
	}
	if !strings.Contains(err.Error(), "EOF") {
		t.Errorf("expected EOF-related error, got: %v", err)
	}
}

func TestMergeSidecar_EmptyDescription(t *testing.T) {
	export := &schema.CtxExport{
		Entities: []schema.Entity{
			{Name: "users", Description: "original desc", Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "email", Type: "text", Description: "original field desc"},
			}},
		},
	}

	sc := &Sidecar{
		Entities: map[string]SidecarEntity{
			"users": {
				Description: "",
				Fields: map[string]SidecarField{
					"email": {Description: ""},
				},
			},
		},
	}

	MergeSidecar(export, sc)

	if export.Entities[0].Description != "original desc" {
		t.Errorf("empty sidecar description should not clobber existing, got %q", export.Entities[0].Description)
	}
	if export.Entities[0].Fields[1].Description != "original field desc" {
		t.Errorf("empty sidecar field description should not clobber existing, got %q", export.Entities[0].Fields[1].Description)
	}
}

func TestMergeSidecar_Warnings(t *testing.T) {
	export := &schema.CtxExport{
		Entities: []schema.Entity{
			{Name: "users", Description: "desc", Fields: []schema.Field{
				{Name: "id", Type: "uuid"},
			}},
		},
	}

	sc := &Sidecar{
		Entities: map[string]SidecarEntity{
			"users": {
				Fields: map[string]SidecarField{
					"nonexistent_field": {Description: "does not exist"},
				},
			},
			"nonexistent_entity": {Description: "does not exist"},
		},
	}

	warnings := MergeSidecar(export, sc)
	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d: %v", len(warnings), warnings)
	}
}
