//go:build integration

package extract_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/valkdb/dbdense/internal/extract"
)

func TestMongoExtractor_Integration(t *testing.T) {
	export := mongoExport(t)

	// Verify we got the expected collections.
	entityMap := make(map[string]bool, len(export.Entities))
	for _, e := range export.Entities {
		entityMap[e.Name] = true
	}

	for _, want := range []string{"users", "products", "orders", "order_items"} {
		if !entityMap[want] {
			t.Errorf("missing collection %q", want)
		}
	}

	// Verify fields were inferred from sampled documents.
	for _, e := range export.Entities {
		if e.Name == "users" {
			if e.Type != "collection" {
				t.Errorf("users type = %q, want %q", e.Type, "collection")
			}
			fieldMap := make(map[string]bool, len(e.Fields))
			for _, f := range e.Fields {
				fieldMap[f.Name] = true
			}
			for _, want := range []string{"_id", "email", "name", "role"} {
				if !fieldMap[want] {
					t.Errorf("users missing inferred field %q", want)
				}
			}
			// _id should be PK.
			for _, f := range e.Fields {
				if f.Name == "_id" && !f.IsPK {
					t.Error("_id should be PK")
				}
			}
		}
		if e.Name == "orders" {
			fieldMap := make(map[string]bool, len(e.Fields))
			for _, f := range e.Fields {
				fieldMap[f.Name] = true
			}
			if !fieldMap["payload"] {
				t.Errorf("orders missing inferred field %q", "payload")
			}
		}
	}
}

func TestMongoExtractor_Indexes(t *testing.T) {
	export := mongoExport(t)

	for _, e := range export.Entities {
		if e.Name == "users" {
			// Should have idx_email (we skip the default _id_ index).
			if len(e.AccessPaths) < 1 {
				t.Errorf("users: expected at least 1 index, got %d", len(e.AccessPaths))
			}
			found := false
			for _, ap := range e.AccessPaths {
				if ap.Name == "idx_email" {
					found = true
					if !ap.IsUnique {
						t.Error("idx_email should be unique")
					}
				}
			}
			if !found {
				t.Error("missing index idx_email on users")
			}
		}
		if e.Name == "orders" {
			found := false
			for _, ap := range e.AccessPaths {
				if ap.Name == "idx_payload_status" {
					found = true
				}
			}
			if !found {
				t.Error("missing index idx_payload_status on orders")
			}
		}
	}
}

func TestMongoExtractor_InferredEdges(t *testing.T) {
	export := mongoExport(t)

	// Seeded collections are pluralized ("users", "orders", "products"),
	// and exact *_id base names ("user", "order", "product") don't match.
	// No edges should be emitted — but high-confidence objectId warnings
	// should be surfaced for operator review.
	if len(export.Edges) != 0 {
		t.Fatalf("expected 0 inferred edges without pluralization, got %d: %+v", len(export.Edges), export.Edges)
	}
}

func TestMongoExtractor_WarningsForSkippedRefs(t *testing.T) {
	seedMongo(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ext := &extract.MongoExtractor{
		URI:      mongoURI(t),
		Database: "dbdense_test",
	}
	if _, err := ext.Extract(ctx); err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(ext.Warnings()) == 0 {
		t.Fatal("expected warnings for unresolved *_id inferred refs")
	}

	// orders.user_id should produce a high-confidence objectId warning
	// since "user" doesn't match any collection.
	foundOrderUser := false
	for _, w := range ext.Warnings() {
		if strings.Contains(w, `orders.user_id`) {
			foundOrderUser = true
			break
		}
	}
	if !foundOrderUser {
		t.Fatalf("expected warning mentioning orders.user_id, got %v", ext.Warnings())
	}
}

func TestMongoExtractor_Subfields(t *testing.T) {
	export := mongoExport(t)

	for _, e := range export.Entities {
		if e.Name == "orders" {
			for _, f := range e.Fields {
				if f.Name == "payload" {
					if len(f.Subfields) == 0 {
						t.Fatal("orders.payload should have subfields")
					}
					sfMap := make(map[string]string, len(f.Subfields))
					for _, sf := range f.Subfields {
						sfMap[sf.Name] = sf.Type
					}
					// The seeded orders.payload has these keys.
					for _, want := range []string{"status", "channel", "items", "shipping"} {
						if _, ok := sfMap[want]; !ok {
							t.Errorf("orders.payload missing subfield %q (got %v)", want, sfMap)
						}
					}
					if sfMap["status"] != "string" {
						t.Errorf("orders.payload.status type = %q, want string", sfMap["status"])
					}
					return
				}
			}
			t.Fatal("orders entity missing payload field")
		}
	}
	t.Fatal("missing orders entity")
}

func TestMongoExtractor_SubfieldsPayments(t *testing.T) {
	export := mongoExport(t)

	for _, e := range export.Entities {
		if e.Name == "payments" {
			for _, f := range e.Fields {
				if f.Name == "gateway_payload" {
					if len(f.Subfields) == 0 {
						t.Fatal("payments.gateway_payload should have subfields")
					}
					sfMap := make(map[string]string, len(f.Subfields))
					for _, sf := range f.Subfields {
						sfMap[sf.Name] = sf.Type
					}
					if _, ok := sfMap["currency"]; !ok {
						t.Errorf("payments.gateway_payload missing subfield 'currency' (got %v)", sfMap)
					}
					return
				}
			}
			t.Fatal("payments entity missing gateway_payload field")
		}
	}
	t.Fatal("missing payments entity")
}
