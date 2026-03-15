//go:build integration

package extract_test

import (
	"testing"
)

func TestPostgresExtractor_Integration(t *testing.T) {
	export := postgresExport(t)

	// Verify we got the expected tables from public schema.
	entityMap := make(map[string]bool, len(export.Entities))
	for _, e := range export.Entities {
		entityMap[e.Name] = true
	}

	for _, want := range []string{"users", "products", "orders", "order_items"} {
		if !entityMap[want] {
			t.Errorf("missing entity %q", want)
		}
	}

	// Public schema may grow over time; require at least the core set.
	if len(export.Entities) < 4 {
		t.Errorf("expected at least 4 entities, got %d", len(export.Entities))
	}

	// Verify FK edges exist.
	if len(export.Edges) == 0 {
		t.Fatal("expected at least one FK edge")
	}

	edgeFound := false
	for _, e := range export.Edges {
		if e.FromEntity == "orders" && e.FromField == "user_id" && e.ToEntity == "users" && e.ToField == "id" {
			edgeFound = true
		}
	}
	if !edgeFound {
		t.Error("missing FK edge orders.user_id → users.id")
	}

	// Verify fields for users table.
	for _, e := range export.Entities {
		if e.Name == "users" {
			if e.Description != "Core identity table." {
				t.Errorf("users description = %q, want %q", e.Description, "Core identity table.")
			}
			fieldMap := make(map[string]bool, len(e.Fields))
			for _, f := range e.Fields {
				fieldMap[f.Name] = true
			}
			for _, want := range []string{"id", "email", "name", "deleted_at"} {
				if !fieldMap[want] {
					t.Errorf("users missing field %q", want)
				}
			}
			// Check PK.
			for _, f := range e.Fields {
				if f.Name == "id" && !f.IsPK {
					t.Error("users.id should be PK")
				}
			}
		}
	}

	// Verify orders has JSONB payload field.
	ordersFound := false
	for _, e := range export.Entities {
		if e.Name != "orders" {
			continue
		}
		ordersFound = true
		fieldMap := make(map[string]bool, len(e.Fields))
		for _, f := range e.Fields {
			fieldMap[f.Name] = true
		}
		if !fieldMap["payload"] {
			t.Error("orders missing field \"payload\"")
		}
	}
	if !ordersFound {
		t.Error("missing orders entity")
	}
}

func TestPostgresExtractor_MultiSchema(t *testing.T) {
	export := postgresExportMultiSchema(t)

	entityMap := make(map[string]bool, len(export.Entities))
	for _, e := range export.Entities {
		entityMap[e.Name] = true
	}

	// Public tables should be unqualified.
	if !entityMap["users"] {
		t.Error("missing entity 'users' (public schema)")
	}

	// Auth tables should be schema-qualified.
	if !entityMap["auth.sessions"] {
		t.Error("missing entity 'auth.sessions'")
	}

	// Multi-schema exports may include extra tables; require at least core+auth.
	if len(export.Entities) < 5 {
		t.Errorf("expected at least 5 entities, got %d", len(export.Entities))
	}
}

func TestPostgresExtractor_Indexes(t *testing.T) {
	export := postgresExport(t)

	// orders should have idx_orders_user_id and idx_orders_status.
	for _, e := range export.Entities {
		if e.Name == "orders" {
			if len(e.AccessPaths) < 2 {
				t.Errorf("orders: expected at least 2 indexes, got %d", len(e.AccessPaths))
			}
			idxNames := make(map[string]bool, len(e.AccessPaths))
			for _, ap := range e.AccessPaths {
				idxNames[ap.Name] = true
			}
			if !idxNames["idx_orders_user_id"] {
				t.Error("missing index idx_orders_user_id")
			}
			if !idxNames["idx_orders_status"] {
				t.Error("missing index idx_orders_status")
			}
			if !idxNames["idx_orders_payload"] {
				t.Error("missing index idx_orders_payload")
			}
		}
	}
}
