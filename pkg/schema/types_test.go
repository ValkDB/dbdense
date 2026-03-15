package schema

import (
	"strings"
	"testing"
)

func TestValidate_Valid(t *testing.T) {
	export := &CtxExport{
		Version: "ctxexport.v0",
		Entities: []Entity{
			{Name: "users", Fields: []Field{{Name: "id", IsPK: true}}},
			{Name: "orders", Fields: []Field{{Name: "id", IsPK: true}, {Name: "user_id"}}},
		},
		Edges: []Edge{{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"}},
	}
	if err := export.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_EmptyVersion(t *testing.T) {
	export := &CtxExport{Version: ""}
	if err := export.Validate(); err == nil {
		t.Error("expected error for empty version")
	}
}

func TestValidate_EmptyEntityName(t *testing.T) {
	export := &CtxExport{
		Version:  "v0",
		Entities: []Entity{{Name: ""}},
	}
	if err := export.Validate(); err == nil {
		t.Error("expected error for empty entity name")
	}
}

func TestValidate_DuplicateEntity(t *testing.T) {
	export := &CtxExport{
		Version:  "v0",
		Entities: []Entity{{Name: "users"}, {Name: "users"}},
	}
	if err := export.Validate(); err == nil {
		t.Error("expected error for duplicate entity")
	}
}

func TestValidate_UnknownEdgeEntity(t *testing.T) {
	export := &CtxExport{
		Version:  "v0",
		Entities: []Entity{{Name: "users"}},
		Edges:    []Edge{{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"}},
	}
	if err := export.Validate(); err == nil {
		t.Error("expected error for unknown edge entity")
	}
}

func TestValidate_EmptyEdgeField(t *testing.T) {
	export := &CtxExport{
		Version:  "v0",
		Entities: []Entity{{Name: "users"}, {Name: "orders"}},
		Edges:    []Edge{{FromEntity: "orders", FromField: "", ToEntity: "users", ToField: "id"}},
	}
	err := export.Validate()
	if err == nil {
		t.Fatal("expected error for empty edge field")
	}
	if !strings.Contains(err.Error(), "empty field reference") {
		t.Errorf("expected 'empty field reference' in error, got: %v", err)
	}
}

// --- ValidateStrict tests ---

func TestValidateStrict_Valid(t *testing.T) {
	export := &CtxExport{
		Version: "ctxexport.v0",
		Entities: []Entity{
			{Name: "users", Fields: []Field{
				{Name: "id", IsPK: true},
				{Name: "email"},
			}},
			{Name: "orders", Fields: []Field{
				{Name: "id", IsPK: true},
				{Name: "user_id"},
			}},
		},
		Edges: []Edge{{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"}},
	}
	if err := export.ValidateStrict(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStrict_DuplicateField(t *testing.T) {
	export := &CtxExport{
		Version: "v0",
		Entities: []Entity{
			{Name: "users", Fields: []Field{
				{Name: "id", IsPK: true},
				{Name: "email"},
				{Name: "email"},
			}},
		},
	}
	err := export.ValidateStrict()
	if err == nil {
		t.Fatal("expected error for duplicate field")
	}
	if !strings.Contains(err.Error(), "duplicate field") {
		t.Errorf("expected 'duplicate field' in error, got: %v", err)
	}
}

func TestValidateStrict_EmptyFieldName(t *testing.T) {
	export := &CtxExport{
		Version: "v0",
		Entities: []Entity{
			{Name: "users", Fields: []Field{{Name: ""}}},
		},
	}
	err := export.ValidateStrict()
	if err == nil {
		t.Fatal("expected error for empty field name")
	}
	if !strings.Contains(err.Error(), "empty name") {
		t.Errorf("expected 'empty name' in error, got: %v", err)
	}
}

func TestValidateStrict_EdgeFieldNotInFromEntity(t *testing.T) {
	export := &CtxExport{
		Version: "v0",
		Entities: []Entity{
			{Name: "users", Fields: []Field{{Name: "id", IsPK: true}}},
			{Name: "orders", Fields: []Field{{Name: "id", IsPK: true}}},
		},
		Edges: []Edge{{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"}},
	}
	err := export.ValidateStrict()
	if err == nil {
		t.Fatal("expected error for missing FromField")
	}
	if !strings.Contains(err.Error(), "user_id") {
		t.Errorf("expected 'user_id' in error, got: %v", err)
	}
}

func TestValidateStrict_EdgeFieldNotInToEntity(t *testing.T) {
	export := &CtxExport{
		Version: "v0",
		Entities: []Entity{
			{Name: "users", Fields: []Field{{Name: "uuid", IsPK: true}}},
			{Name: "orders", Fields: []Field{{Name: "id", IsPK: true}, {Name: "user_id"}}},
		},
		Edges: []Edge{{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id"}},
	}
	err := export.ValidateStrict()
	if err == nil {
		t.Fatal("expected error for missing ToField")
	}
	if !strings.Contains(err.Error(), "not found in entity \"users\"") {
		t.Errorf("expected field-not-found error, got: %v", err)
	}
}

func TestValidateStrict_AccessPathUnknownColumn(t *testing.T) {
	export := &CtxExport{
		Version: "v0",
		Entities: []Entity{
			{Name: "users", Fields: []Field{{Name: "id", IsPK: true}}, AccessPaths: []AccessPath{
				{Name: "idx_email", Columns: []string{"email"}, IsUnique: true},
			}},
		},
	}
	err := export.ValidateStrict()
	if err == nil {
		t.Fatal("expected error for unknown access path column")
	}
	if !strings.Contains(err.Error(), "unknown column \"email\"") {
		t.Errorf("expected 'unknown column' error, got: %v", err)
	}
}

func TestValidateStrict_AccessPathValidColumn(t *testing.T) {
	export := &CtxExport{
		Version: "v0",
		Entities: []Entity{
			{Name: "users", Fields: []Field{
				{Name: "id", IsPK: true},
				{Name: "email"},
			}, AccessPaths: []AccessPath{
				{Name: "idx_email", Columns: []string{"email"}, IsUnique: true},
			}},
		},
	}
	if err := export.ValidateStrict(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStrict_BasicValidationStillRuns(t *testing.T) {
	// ValidateStrict should catch basic issues like empty version.
	export := &CtxExport{Version: ""}
	if err := export.ValidateStrict(); err == nil {
		t.Error("expected error for empty version via ValidateStrict")
	}
}
