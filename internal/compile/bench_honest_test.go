package compile

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/valkdb/dbdense/pkg/schema"
)

// ---------------------------------------------------------------------------
// Token estimation constants
// ---------------------------------------------------------------------------

// charsPerTokenHeuristic is the rough heuristic used throughout this file.
// It systematically underestimates real token counts for SQL DDL. See the
// disclaimer printed by TestHonestMetrics.
const charsPerTokenHeuristic = 4

// tokenBiasMultiplier corrects for the known optimism of chars/4. Multiply
// estimated tokens by this to get a conservative real-world estimate.
// Based on empirical measurement: SQL keywords like CREATE, TABLE, ALTER are
// 1 token each but 5-6 chars; short identifiers are also 1 token.
const tokenBiasMultiplier = 1.2

// ---------------------------------------------------------------------------
// Fixture builders — deterministic, realistic, reusable
// ---------------------------------------------------------------------------

// startupSaaSExport builds a 30-table schema resembling a typical startup
// SaaS product. Hand-authored field lists with realistic types, counts, and
// FK density. No randomness — fully deterministic.
func startupSaaSExport() *schema.CtxExport {
	entities := []schema.Entity{
		{Name: "users", Type: "table", Description: "Core user identity.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "email", Type: "character varying(255)", NotNull: true, Description: "Login email, unique."},
			{Name: "name", Type: "text"},
			{Name: "password_hash", Type: "text", NotNull: true},
			{Name: "avatar_url", Type: "text"},
			{Name: "is_active", Type: "boolean", NotNull: true, Default: "true"},
			{Name: "last_login_at", Type: "timestamptz"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
			{Name: "updated_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_users_email", Columns: []string{"email"}, IsUnique: true},
			{Name: "idx_users_created_at", Columns: []string{"created_at"}, IsUnique: false},
		}},
		{Name: "organizations", Type: "table", Description: "Multi-tenant organizations.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "name", Type: "text", NotNull: true},
			{Name: "slug", Type: "character varying(100)", NotNull: true},
			{Name: "billing_email", Type: "text"},
			{Name: "settings", Type: "jsonb", Default: "'{}'::jsonb"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_organizations_slug", Columns: []string{"slug"}, IsUnique: true},
		}},
		{Name: "memberships", Type: "table", Description: "User-to-org membership with role.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid", NotNull: true},
			{Name: "organization_id", Type: "uuid", NotNull: true},
			{Name: "role", Type: "text", NotNull: true, Default: "'member'", Description: "admin, member, viewer."},
			{Name: "invited_at", Type: "timestamptz"},
			{Name: "accepted_at", Type: "timestamptz"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_memberships_user_org", Columns: []string{"user_id", "organization_id"}, IsUnique: true},
		}},
		{Name: "plans", Type: "table", Description: "Subscription plan definitions.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "name", Type: "text", NotNull: true},
			{Name: "price_monthly", Type: "numeric(10,2)", NotNull: true},
			{Name: "price_yearly", Type: "numeric(10,2)"},
			{Name: "max_seats", Type: "integer"},
			{Name: "features", Type: "jsonb"},
			{Name: "is_active", Type: "boolean", NotNull: true, Default: "true"},
		}},
		{Name: "subscriptions", Type: "table", Description: "Active subscriptions tying orgs to plans.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "organization_id", Type: "uuid", NotNull: true},
			{Name: "plan_id", Type: "uuid", NotNull: true},
			{Name: "status", Type: "text", NotNull: true, Description: "active, past_due, cancelled."},
			{Name: "current_period_start", Type: "timestamptz", NotNull: true},
			{Name: "current_period_end", Type: "timestamptz", NotNull: true},
			{Name: "cancelled_at", Type: "timestamptz"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_subscriptions_org_id", Columns: []string{"organization_id"}, IsUnique: false},
			{Name: "idx_subscriptions_status", Columns: []string{"status"}, IsUnique: false},
		}},
		{Name: "invoices", Type: "table", Description: "Billing invoices.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "subscription_id", Type: "uuid", NotNull: true},
			{Name: "amount", Type: "numeric(10,2)", NotNull: true},
			{Name: "currency", Type: "character varying(3)", NotNull: true, Default: "'USD'"},
			{Name: "status", Type: "text", NotNull: true, Description: "draft, open, paid, void."},
			{Name: "due_date", Type: "date"},
			{Name: "paid_at", Type: "timestamptz"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}},
		{Name: "payments", Type: "table", Description: "Payment transactions.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "invoice_id", Type: "uuid", NotNull: true},
			{Name: "amount", Type: "numeric(10,2)", NotNull: true},
			{Name: "provider", Type: "text", NotNull: true, Description: "stripe, paypal."},
			{Name: "provider_txn_id", Type: "text"},
			{Name: "status", Type: "text", NotNull: true, Description: "pending, succeeded, failed, refunded."},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}},
		{Name: "products", Type: "table", Description: "Product catalog.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "name", Type: "text", NotNull: true},
			{Name: "description", Type: "text"},
			{Name: "sku", Type: "character varying(50)", NotNull: true},
			{Name: "price", Type: "numeric(10,2)", NotNull: true},
			{Name: "currency", Type: "character varying(3)", NotNull: true, Default: "'USD'"},
			{Name: "is_active", Type: "boolean", NotNull: true, Default: "true"},
			{Name: "metadata", Type: "jsonb"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_products_sku", Columns: []string{"sku"}, IsUnique: true},
			{Name: "idx_products_name", Columns: []string{"name"}, IsUnique: false},
		}},
		{Name: "orders", Type: "table", Description: "Customer orders.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid", NotNull: true},
			{Name: "organization_id", Type: "uuid"},
			{Name: "status", Type: "text", NotNull: true, Description: "pending, confirmed, shipped, delivered, cancelled."},
			{Name: "total", Type: "numeric(10,2)", NotNull: true},
			{Name: "currency", Type: "character varying(3)", NotNull: true, Default: "'USD'"},
			{Name: "notes", Type: "text"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
			{Name: "updated_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_orders_user_id", Columns: []string{"user_id"}, IsUnique: false},
			{Name: "idx_orders_status", Columns: []string{"status"}, IsUnique: false},
			{Name: "idx_orders_created_at", Columns: []string{"created_at"}, IsUnique: false},
		}},
		{Name: "order_items", Type: "table", Description: "Line items within an order.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "order_id", Type: "uuid", NotNull: true},
			{Name: "product_id", Type: "uuid", NotNull: true},
			{Name: "quantity", Type: "integer", NotNull: true},
			{Name: "unit_price", Type: "numeric(10,2)", NotNull: true},
			{Name: "subtotal", Type: "numeric(10,2)", NotNull: true},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_order_items_order_id", Columns: []string{"order_id"}, IsUnique: false},
		}},
		{Name: "carts", Type: "table", Description: "Shopping carts.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid", NotNull: true},
			{Name: "expires_at", Type: "timestamptz"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}},
		{Name: "cart_items", Type: "table", Description: "Items in a shopping cart.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "cart_id", Type: "uuid", NotNull: true},
			{Name: "product_id", Type: "uuid", NotNull: true},
			{Name: "quantity", Type: "integer", NotNull: true, Default: "1"},
		}},
		{Name: "reviews", Type: "table", Description: "Product reviews by users.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid", NotNull: true},
			{Name: "product_id", Type: "uuid", NotNull: true},
			{Name: "rating", Type: "integer", NotNull: true, Description: "1-5 stars."},
			{Name: "body", Type: "text"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}},
		{Name: "categories", Type: "table", Description: "Product categories.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "name", Type: "text", NotNull: true},
			{Name: "parent_id", Type: "uuid", Description: "Self-referencing FK for hierarchy."},
			{Name: "sort_order", Type: "integer", Default: "0"},
		}},
		{Name: "tags", Type: "table", Description: "Free-form tags.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "name", Type: "text", NotNull: true},
		}},
		{Name: "product_tags", Type: "table", Description: "Many-to-many product-tag junction.", Fields: []schema.Field{
			{Name: "product_id", Type: "uuid", IsPK: true},
			{Name: "tag_id", Type: "uuid", IsPK: true},
		}},
		{Name: "addresses", Type: "table", Description: "User addresses for shipping/billing.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid", NotNull: true},
			{Name: "type", Type: "text", NotNull: true, Description: "shipping, billing."},
			{Name: "line1", Type: "text", NotNull: true},
			{Name: "line2", Type: "text"},
			{Name: "city", Type: "text", NotNull: true},
			{Name: "state", Type: "text"},
			{Name: "postal_code", Type: "character varying(20)"},
			{Name: "country", Type: "character varying(2)", NotNull: true},
			{Name: "is_default", Type: "boolean", NotNull: true, Default: "false"},
		}},
		{Name: "shipments", Type: "table", Description: "Shipment tracking.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "order_id", Type: "uuid", NotNull: true},
			{Name: "address_id", Type: "uuid", NotNull: true},
			{Name: "carrier", Type: "text"},
			{Name: "tracking_number", Type: "text"},
			{Name: "status", Type: "text", NotNull: true, Description: "pending, shipped, in_transit, delivered."},
			{Name: "shipped_at", Type: "timestamptz"},
			{Name: "delivered_at", Type: "timestamptz"},
		}},
		{Name: "notifications", Type: "table", Description: "User notifications.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid", NotNull: true},
			{Name: "type", Type: "text", NotNull: true},
			{Name: "title", Type: "text", NotNull: true},
			{Name: "body", Type: "text"},
			{Name: "read_at", Type: "timestamptz"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}},
		{Name: "settings", Type: "table", Description: "Key-value user settings.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid", NotNull: true},
			{Name: "key", Type: "text", NotNull: true},
			{Name: "value", Type: "jsonb"},
		}},
		{Name: "sessions", Type: "table", Description: "User login sessions.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid", NotNull: true},
			{Name: "token_hash", Type: "text", NotNull: true},
			{Name: "ip_address", Type: "text"},
			{Name: "user_agent", Type: "text"},
			{Name: "expires_at", Type: "timestamptz", NotNull: true},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_sessions_token_hash", Columns: []string{"token_hash"}, IsUnique: true},
			{Name: "idx_sessions_user_id", Columns: []string{"user_id"}, IsUnique: false},
		}},
		{Name: "api_keys", Type: "table", Description: "API keys for programmatic access.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "organization_id", Type: "uuid", NotNull: true},
			{Name: "name", Type: "text", NotNull: true},
			{Name: "key_hash", Type: "text", NotNull: true},
			{Name: "last_used_at", Type: "timestamptz"},
			{Name: "expires_at", Type: "timestamptz"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_api_keys_key_hash", Columns: []string{"key_hash"}, IsUnique: true},
			{Name: "idx_api_keys_org_id", Columns: []string{"organization_id"}, IsUnique: false},
		}},
		{Name: "webhooks", Type: "table", Description: "Webhook endpoint registrations.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "organization_id", Type: "uuid", NotNull: true},
			{Name: "url", Type: "text", NotNull: true},
			{Name: "events", Type: "text[]", NotNull: true, Description: "Array of event types to subscribe."},
			{Name: "secret", Type: "text", NotNull: true},
			{Name: "is_active", Type: "boolean", NotNull: true, Default: "true"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}},
		{Name: "events", Type: "table", Description: "Domain events for event sourcing.", Fields: []schema.Field{
			{Name: "id", Type: "bigint", IsPK: true},
			{Name: "aggregate_type", Type: "text", NotNull: true},
			{Name: "aggregate_id", Type: "uuid", NotNull: true},
			{Name: "event_type", Type: "text", NotNull: true},
			{Name: "payload", Type: "jsonb", NotNull: true},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_events_aggregate", Columns: []string{"aggregate_type", "aggregate_id"}, IsUnique: false},
			{Name: "idx_events_created_at", Columns: []string{"created_at"}, IsUnique: false},
		}},
		{Name: "audit_log", Type: "table", Description: "System audit trail.", Fields: []schema.Field{
			{Name: "id", Type: "bigint", IsPK: true},
			{Name: "actor_id", Type: "uuid"},
			{Name: "action", Type: "text", NotNull: true},
			{Name: "resource_type", Type: "text", NotNull: true},
			{Name: "resource_id", Type: "uuid"},
			{Name: "changes", Type: "jsonb"},
			{Name: "ip_address", Type: "text"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_audit_log_actor_id", Columns: []string{"actor_id"}, IsUnique: false},
			{Name: "idx_audit_log_resource", Columns: []string{"resource_type", "resource_id"}, IsUnique: false},
			{Name: "idx_audit_log_created_at", Columns: []string{"created_at"}, IsUnique: false},
		}},
		{Name: "migrations", Type: "table", Description: "Schema migration tracking.", Fields: []schema.Field{
			{Name: "id", Type: "integer", IsPK: true},
			{Name: "version", Type: "character varying(255)", NotNull: true},
			{Name: "applied_at", Type: "timestamptz", NotNull: true, Default: "now()"},
			{Name: "checksum", Type: "text"},
		}},
		{Name: "feature_flags", Type: "table", Description: "Feature flag definitions.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "name", Type: "text", NotNull: true},
			{Name: "description", Type: "text"},
			{Name: "is_enabled", Type: "boolean", NotNull: true, Default: "false"},
			{Name: "rules", Type: "jsonb", Description: "Targeting rules as JSON."},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}},
		{Name: "ab_tests", Type: "table", Description: "A/B test experiments.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "feature_flag_id", Type: "uuid", NotNull: true},
			{Name: "name", Type: "text", NotNull: true},
			{Name: "variant_weights", Type: "jsonb", NotNull: true},
			{Name: "started_at", Type: "timestamptz"},
			{Name: "ended_at", Type: "timestamptz"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}},
		{Name: "support_tickets", Type: "table", Description: "Customer support tickets.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "user_id", Type: "uuid", NotNull: true},
			{Name: "organization_id", Type: "uuid"},
			{Name: "subject", Type: "text", NotNull: true},
			{Name: "body", Type: "text", NotNull: true},
			{Name: "status", Type: "text", NotNull: true, Default: "'open'", Description: "open, in_progress, resolved, closed."},
			{Name: "priority", Type: "text", NotNull: true, Default: "'normal'", Description: "low, normal, high, urgent."},
			{Name: "assigned_to", Type: "uuid"},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
			{Name: "updated_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}, AccessPaths: []schema.AccessPath{
			{Name: "idx_support_tickets_user_id", Columns: []string{"user_id"}, IsUnique: false},
			{Name: "idx_support_tickets_status", Columns: []string{"status"}, IsUnique: false},
		}},
		{Name: "attachments", Type: "table", Description: "File attachments for tickets and other entities.", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "support_ticket_id", Type: "uuid"},
			{Name: "file_name", Type: "text", NotNull: true},
			{Name: "file_size", Type: "bigint", NotNull: true},
			{Name: "content_type", Type: "text", NotNull: true},
			{Name: "storage_path", Type: "text", NotNull: true},
			{Name: "uploaded_by", Type: "uuid", NotNull: true},
			{Name: "created_at", Type: "timestamptz", NotNull: true, Default: "now()"},
		}},
	}

	// 25 FKs — realistic edge density for a 30-table SaaS schema.
	edges := []schema.Edge{
		{FromEntity: "memberships", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "memberships", FromField: "organization_id", ToEntity: "organizations", ToField: "id", Type: "foreign_key"},
		{FromEntity: "subscriptions", FromField: "organization_id", ToEntity: "organizations", ToField: "id", Type: "foreign_key"},
		{FromEntity: "subscriptions", FromField: "plan_id", ToEntity: "plans", ToField: "id", Type: "foreign_key"},
		{FromEntity: "invoices", FromField: "subscription_id", ToEntity: "subscriptions", ToField: "id", Type: "foreign_key"},
		{FromEntity: "payments", FromField: "invoice_id", ToEntity: "invoices", ToField: "id", Type: "foreign_key"},
		{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "orders", FromField: "organization_id", ToEntity: "organizations", ToField: "id", Type: "foreign_key"},
		{FromEntity: "order_items", FromField: "order_id", ToEntity: "orders", ToField: "id", Type: "foreign_key"},
		{FromEntity: "order_items", FromField: "product_id", ToEntity: "products", ToField: "id", Type: "foreign_key"},
		{FromEntity: "carts", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "cart_items", FromField: "cart_id", ToEntity: "carts", ToField: "id", Type: "foreign_key"},
		{FromEntity: "cart_items", FromField: "product_id", ToEntity: "products", ToField: "id", Type: "foreign_key"},
		{FromEntity: "reviews", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "reviews", FromField: "product_id", ToEntity: "products", ToField: "id", Type: "foreign_key"},
		{FromEntity: "categories", FromField: "parent_id", ToEntity: "categories", ToField: "id", Type: "foreign_key"},
		{FromEntity: "product_tags", FromField: "product_id", ToEntity: "products", ToField: "id", Type: "foreign_key"},
		{FromEntity: "product_tags", FromField: "tag_id", ToEntity: "tags", ToField: "id", Type: "foreign_key"},
		{FromEntity: "addresses", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "shipments", FromField: "order_id", ToEntity: "orders", ToField: "id", Type: "foreign_key"},
		{FromEntity: "shipments", FromField: "address_id", ToEntity: "addresses", ToField: "id", Type: "foreign_key"},
		{FromEntity: "notifications", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "settings", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "sessions", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "api_keys", FromField: "organization_id", ToEntity: "organizations", ToField: "id", Type: "foreign_key"},
		{FromEntity: "webhooks", FromField: "organization_id", ToEntity: "organizations", ToField: "id", Type: "foreign_key"},
		{FromEntity: "audit_log", FromField: "actor_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "ab_tests", FromField: "feature_flag_id", ToEntity: "feature_flags", ToField: "id", Type: "foreign_key"},
		{FromEntity: "support_tickets", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "support_tickets", FromField: "organization_id", ToEntity: "organizations", ToField: "id", Type: "foreign_key"},
		{FromEntity: "attachments", FromField: "support_ticket_id", ToEntity: "support_tickets", ToField: "id", Type: "foreign_key"},
		{FromEntity: "attachments", FromField: "uploaded_by", ToEntity: "users", ToField: "id", Type: "foreign_key"},
	}

	return &schema.CtxExport{
		Version:  "ctxexport.v0",
		Entities: entities,
		Edges:    edges,
	}
}

// enterpriseERPExport builds a 200-table schema resembling an enterprise ERP.
// Uses gen-style noise tables but with realistic ERP naming conventions:
// dim_ (dimensions), fact_ (facts), stg_ (staging), ref_ (reference),
// xref_ (cross-reference). Schema-qualified names simulate multi-schema dbs.
// Field counts are 10-30 per table. Higher edge density than gen.go defaults.
//
// Deterministic: uses fixed seed.
func enterpriseERPExport() *schema.CtxExport {
	const (
		totalTables = 200
		seed        = 12345
		// erpMinFields is the minimum number of fields per ERP table.
		erpMinFields = 10
		// erpFieldRange is added via rng.Intn, so max fields = erpMinFields + erpFieldRange - 1 = 30.
		erpFieldRange = 21
		// erpEdgeProbability is the chance each table gets an FK to a prior table.
		// Higher than gen.go's 0.3 to simulate denser enterprise schemas.
		erpEdgeProbability = 0.5
	)

	rng := rand.New(rand.NewSource(seed))

	erpPrefixes := []string{"dim", "fact", "stg", "ref", "xref", "rpt", "agg", "hist", "log", "cfg"}
	erpDomains := []string{
		"customer", "employee", "vendor", "product", "warehouse", "shipment",
		"invoice", "payment", "purchase_order", "sales_order", "gl_account",
		"cost_center", "department", "project", "budget", "contract",
		"asset", "inventory", "production", "quality",
	}
	erpSchemas := []string{"sales", "hr", "finance", "warehouse", "production", "public"}

	erpFieldNames := []string{
		"id", "code", "name", "description", "status", "type", "category",
		"amount", "quantity", "unit_price", "total_amount", "currency_code",
		"effective_date", "expiry_date", "created_at", "updated_at",
		"created_by", "updated_by", "is_active", "is_deleted",
		"external_id", "reference_code", "batch_id", "sequence_number",
		"remarks", "metadata", "tags", "version", "checksum", "priority",
	}
	erpFieldTypes := []string{
		"uuid", "text", "integer", "bigint", "boolean", "timestamptz",
		"numeric(12,2)", "numeric(10,4)", "character varying(100)",
		"character varying(255)", "jsonb", "date",
	}

	entities := make([]schema.Entity, 0, totalTables)
	usedNames := make(map[string]bool, totalTables)
	allNames := make([]string, 0, totalTables)

	for i := 0; i < totalTables; i++ {
		prefix := erpPrefixes[rng.Intn(len(erpPrefixes))]
		domain := erpDomains[rng.Intn(len(erpDomains))]
		schemaName := erpSchemas[rng.Intn(len(erpSchemas))]

		name := fmt.Sprintf("%s.%s_%s", schemaName, prefix, domain)
		// Ensure uniqueness by appending index if needed.
		if usedNames[name] {
			name = fmt.Sprintf("%s_%d", name, i)
		}
		usedNames[name] = true
		allNames = append(allNames, name)

		fieldCount := erpMinFields + rng.Intn(erpFieldRange)
		fields := make([]schema.Field, 0, fieldCount)
		fields = append(fields, schema.Field{Name: "id", Type: "uuid", IsPK: true})

		usedFieldNames := make(map[string]bool, fieldCount)
		usedFieldNames["id"] = true
		for j := 1; j < fieldCount; j++ {
			fname := erpFieldNames[rng.Intn(len(erpFieldNames))]
			if usedFieldNames[fname] {
				fname = fmt.Sprintf("%s_%d", fname, j)
			}
			usedFieldNames[fname] = true
			ftype := erpFieldTypes[rng.Intn(len(erpFieldTypes))]
			f := schema.Field{Name: fname, Type: ftype}
			// ~40% of fields get NOT NULL
			if rng.Float64() < 0.4 {
				f.NotNull = true
			}
			fields = append(fields, f)
		}

		// Only ~30% of tables get descriptions in enterprise schemas.
		desc := ""
		if rng.Float64() < 0.3 {
			desc = fmt.Sprintf("Enterprise %s %s table.", prefix, domain)
		}

		// Generate 1-2 indexes per ERP table on non-PK fields.
		var accessPaths []schema.AccessPath
		nonPKFields := make([]schema.Field, 0, len(fields))
		for _, f := range fields {
			if !f.IsPK {
				nonPKFields = append(nonPKFields, f)
			}
		}
		if len(nonPKFields) > 0 {
			// erpIndexCount is 1-2 indexes per table.
			erpIndexCount := 1 + rng.Intn(2)
			if erpIndexCount > len(nonPKFields) {
				erpIndexCount = len(nonPKFields)
			}
			accessPaths = make([]schema.AccessPath, 0, erpIndexCount)
			indexedCols := make(map[string]bool, erpIndexCount)
			for idx := 0; idx < erpIndexCount; idx++ {
				col := nonPKFields[rng.Intn(len(nonPKFields))]
				if indexedCols[col.Name] {
					continue
				}
				indexedCols[col.Name] = true
				accessPaths = append(accessPaths, schema.AccessPath{
					Name:     fmt.Sprintf("idx_%d_%s", i, col.Name),
					Columns:  []string{col.Name},
					IsUnique: rng.Float64() < 0.2,
				})
			}
		}

		entities = append(entities, schema.Entity{
			Name:        name,
			Type:        "table",
			Description: desc,
			Fields:      fields,
			AccessPaths: accessPaths,
		})
	}

	// Build edges with higher density than gen.go.
	edges := make([]schema.Edge, 0, totalTables)
	for i := 1; i < len(allNames); i++ {
		if rng.Float64() < erpEdgeProbability {
			target := allNames[rng.Intn(i)]
			edges = append(edges, schema.Edge{
				FromEntity: allNames[i],
				FromField:  "id", // Simplified — uses id as FK field for benchmark purposes
				ToEntity:   target,
				ToField:    "id",
				Type:       "foreign_key",
			})
		}
		// ~15% get a second FK for denser graphs.
		if rng.Float64() < 0.15 && i > 1 {
			target := allNames[rng.Intn(i)]
			edges = append(edges, schema.Edge{
				FromEntity: allNames[i],
				FromField:  "created_by",
				ToEntity:   target,
				ToField:    "id",
				Type:       "foreign_key",
			})
		}
	}

	return &schema.CtxExport{
		Version:  "ctxexport.v0",
		Entities: entities,
		Edges:    edges,
	}
}

// legacyNightmareExport builds a 500-table schema simulating a legacy database
// with inconsistent naming, mixed conventions, abbreviations, large tables,
// sparse descriptions, and circular FK references.
//
// Deterministic: uses fixed seed.
func legacyNightmareExport() *schema.CtxExport {
	const (
		totalTables = 500
		seed        = 99999
		// legacyMinFields is the minimum number of fields per legacy table.
		legacyMinFields = 5
		// legacyFieldRange gives max fields = legacyMinFields + legacyFieldRange - 1 = 39.
		// ~10% of tables get 30+ fields via the wide table boost below.
		legacyFieldRange = 35
		// legacyEdgeProbability is the base FK probability.
		legacyEdgeProbability = 0.35
		// legacyDescriptionRate is the chance a table gets a description.
		// Legacy schemas have sparse documentation.
		legacyDescriptionRate = 0.10
		// legacyWideTableRate is the chance a table gets boosted to 30-60 fields.
		legacyWideTableRate = 0.10
		// legacyWideFieldMin is the minimum fields for a wide table.
		legacyWideFieldMin = 30
		// legacyWideFieldRange gives max wide fields = legacyWideFieldMin + legacyWideFieldRange - 1 = 60.
		legacyWideFieldRange = 31
		// legacyCircularEdgeCount is the number of deliberate circular FK refs.
		legacyCircularEdgeCount = 15
	)

	rng := rand.New(rand.NewSource(seed))

	// Mixed naming conventions to simulate legacy mess.
	legacyNames := []string{
		"usr_acct", "UsrAcctXref", "user_account_v2", "tbl_users_backup",
		"CUSTOMER_MASTER", "cust_addr", "CustAddrLnk", "customer_address_history",
		"ord_hdr", "ord_dtl", "OrderHeader_v3", "order_detail_line_item",
		"inv_hdr", "InvoiceDetail", "INV_LINE_ITM", "invoice_payment_xref_v2",
		"prd_cat", "ProductCategory", "product_category_mapping_v2",
		"emp_master", "EmpDepartment", "employee_salary_history",
		"GL_ACCOUNT", "gl_journal_entry", "GlJournalLine",
		"wh_location", "WarehouseBin", "warehouse_inventory_snapshot",
		"po_header", "PurchaseOrderLine", "po_receipt_v2",
		"ship_manifest", "ShippingLabel", "shipping_tracking_event_log",
		"cfg_system", "ConfigParam", "config_audit_trail",
		"tmp_import_batch", "TmpStagingBuffer", "tmp_data_migration_v3",
		"rpt_daily_sales", "RptWeeklyKPI", "report_monthly_summary",
		"ref_country", "RefCurrency", "ref_status_code_lookup",
		"log_app_error", "LogApiCall", "log_background_job_execution",
		"cache_session", "CacheUserPref", "cache_product_search_index",
		"queue_email", "QueueNotification", "queue_async_task_v2",
		"arch_orders_2020", "ArchInvoices2021", "archive_customer_data_v1",
		"dim_time", "DimGeography", "dim_product_hierarchy",
		"fact_sales_daily", "FactInventory", "fact_customer_activity",
		"stg_raw_import", "StgTransform", "stg_validated_records",
		"xref_user_role", "XrefProductSupplier", "xref_order_promotion_v2",
		"sys_schema_version", "SysJobSchedule", "sys_feature_toggle",
		"bak_users_20230101", "BakOrdersQ4", "bak_audit_log_archive",
	}

	legacyFieldNames := []string{
		"id", "ID", "pk_id", "record_id", "guid", "uuid",
		"nm", "name", "full_name", "display_name", "short_desc",
		"cd", "code", "type_cd", "status_cd", "category_cd",
		"val", "value", "amt", "amount", "qty", "quantity",
		"dt", "date", "created_dt", "modified_dt", "effective_dt",
		"flg", "flag", "is_active", "is_deleted", "is_archived",
		"ref", "ref_id", "parent_ref", "ext_ref", "legacy_ref",
		"txt", "notes", "remarks", "description", "comment",
		"payload", "metadata", "extra_data", "attributes", "config",
		"seq", "sort_order", "priority", "version", "checksum",
		"user_id", "created_by", "modified_by", "owner_id", "assigned_to",
	}
	legacyFieldTypes := []string{
		"uuid", "text", "integer", "bigint", "boolean", "timestamp",
		"numeric", "jsonb", "character varying(255)", "character varying(50)",
		"date", "smallint", "double precision", "bytea", "text[]",
	}

	entities := make([]schema.Entity, 0, totalTables)
	allNames := make([]string, 0, totalTables)
	usedNames := make(map[string]bool, totalTables)

	for i := 0; i < totalTables; i++ {
		var name string
		if i < len(legacyNames) {
			name = legacyNames[i]
		} else {
			// Generate additional names with messy patterns.
			patterns := []string{
				"tbl_%s_%d", "%s_xref_v%d", "%s_backup_%d",
				"%s_history_%d", "tmp_%s_%d", "%s_archive_%d",
			}
			domains := []string{
				"customer", "order", "product", "invoice", "payment",
				"employee", "account", "transaction", "report", "config",
			}
			pat := patterns[rng.Intn(len(patterns))]
			dom := domains[rng.Intn(len(domains))]
			name = fmt.Sprintf(pat, dom, i)
		}
		if usedNames[name] {
			name = fmt.Sprintf("%s_%d", name, i)
		}
		usedNames[name] = true
		allNames = append(allNames, name)

		// Determine field count. ~10% are wide tables.
		var fieldCount int
		if rng.Float64() < legacyWideTableRate {
			fieldCount = legacyWideFieldMin + rng.Intn(legacyWideFieldRange)
		} else {
			fieldCount = legacyMinFields + rng.Intn(legacyFieldRange)
		}

		fields := make([]schema.Field, 0, fieldCount)
		fields = append(fields, schema.Field{Name: "id", Type: "bigint", IsPK: true})

		usedFieldNames := make(map[string]bool, fieldCount)
		usedFieldNames["id"] = true
		for j := 1; j < fieldCount; j++ {
			fname := legacyFieldNames[rng.Intn(len(legacyFieldNames))]
			if usedFieldNames[fname] {
				fname = fmt.Sprintf("%s_%d", fname, j)
			}
			usedFieldNames[fname] = true
			ftype := legacyFieldTypes[rng.Intn(len(legacyFieldTypes))]
			f := schema.Field{Name: fname, Type: ftype}
			// 30% NOT NULL
			if rng.Float64() < 0.3 {
				f.NotNull = true
			}
			// 10% get a default
			if rng.Float64() < 0.1 {
				defaults := []string{"0", "''", "false", "now()", "'{}'::jsonb"}
				f.Default = defaults[rng.Intn(len(defaults))]
			}
			fields = append(fields, f)
		}

		desc := ""
		if rng.Float64() < legacyDescriptionRate {
			desc = fmt.Sprintf("Legacy %s table (do not modify without DBA approval).", name)
		}

		// Legacy schemas have inconsistent indexing — ~60% of tables have at least one index.
		var accessPaths []schema.AccessPath
		if rng.Float64() < 0.6 {
			nonPKFields := make([]schema.Field, 0, len(fields))
			for _, f := range fields {
				if !f.IsPK {
					nonPKFields = append(nonPKFields, f)
				}
			}
			if len(nonPKFields) > 0 {
				col := nonPKFields[rng.Intn(len(nonPKFields))]
				accessPaths = []schema.AccessPath{
					{
						Name:     fmt.Sprintf("idx_%s_%s", name, col.Name),
						Columns:  []string{col.Name},
						IsUnique: rng.Float64() < 0.15,
					},
				}
			}
		}

		entities = append(entities, schema.Entity{
			Name:        name,
			Type:        "table",
			Description: desc,
			Fields:      fields,
			AccessPaths: accessPaths,
		})
	}

	// Build edges: regular + circular.
	edges := make([]schema.Edge, 0, totalTables)
	for i := 1; i < len(allNames); i++ {
		if rng.Float64() < legacyEdgeProbability {
			target := allNames[rng.Intn(i)]
			edges = append(edges, schema.Edge{
				FromEntity: allNames[i],
				FromField:  "ref_id",
				ToEntity:   target,
				ToField:    "id",
				Type:       "foreign_key",
			})
		}
	}
	// Circular FK references — a legacy nightmare.
	for i := 0; i < legacyCircularEdgeCount; i++ {
		a := rng.Intn(len(allNames))
		b := rng.Intn(len(allNames))
		if a == b {
			b = (a + 1) % len(allNames)
		}
		edges = append(edges, schema.Edge{
			FromEntity: allNames[a],
			FromField:  "parent_ref",
			ToEntity:   allNames[b],
			ToField:    "id",
			Type:       "foreign_key",
		})
	}

	return &schema.CtxExport{
		Version:  "ctxexport.v0",
		Entities: entities,
		Edges:    edges,
	}
}

// ---------------------------------------------------------------------------
// Naive DDL baseline — what pg_dump would produce for the same schema
// ---------------------------------------------------------------------------

// renderNaiveDDL produces pg_dump-style CREATE TABLE output for the same schema.
// This is more verbose than dbdense DDL: it includes NOT NULL, DEFAULT, CHECK-style
// annotations inline, constraint names, and separate ALTER TABLE for each FK.
// The comparison shows whether dbdense DDL is actually more compact than raw pg_dump.
func renderNaiveDDL(entities []schema.Entity, edges []schema.Edge) string {
	var b strings.Builder

	b.WriteString("--\n-- PostgreSQL database dump\n--\n\n")
	b.WriteString("SET statement_timeout = 0;\n")
	b.WriteString("SET lock_timeout = 0;\n")
	b.WriteString("SET client_encoding = 'UTF8';\n")
	b.WriteString("SET standard_conforming_strings = on;\n")
	b.WriteString("SET check_function_bodies = false;\n")
	b.WriteString("SET client_min_messages = warning;\n\n")

	for _, ent := range entities {
		if ent.Type == "view" {
			fmt.Fprintf(&b, "-- View: %s (definition omitted)\n\n", ent.Name)
			continue
		}

		fmt.Fprintf(&b, "CREATE TABLE %s (\n", ent.Name)

		pks := make([]string, 0, 2)
		for _, f := range ent.Fields {
			if f.IsPK {
				pks = append(pks, f.Name)
			}
		}

		for fi, f := range ent.Fields {
			b.WriteString("    ")
			b.WriteString(f.Name)
			if f.Type != "" {
				b.WriteByte(' ')
				b.WriteString(f.Type)
			}

			if f.NotNull || (f.IsPK && len(pks) == 1) {
				b.WriteString(" NOT NULL")
			}

			if f.Default != "" {
				fmt.Fprintf(&b, " DEFAULT %s", f.Default)
			}

			if fi < len(ent.Fields)-1 || len(pks) > 0 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}

		if len(pks) > 0 {
			fmt.Fprintf(&b, "    CONSTRAINT %s_pkey PRIMARY KEY (%s)\n",
				ent.Name, strings.Join(pks, ", "))
		}

		b.WriteString(");\n\n")

		// Table and column comments.
		if ent.Description != "" {
			fmt.Fprintf(&b, "COMMENT ON TABLE %s IS '%s';\n", ent.Name, ent.Description)
		}
		for _, f := range ent.Fields {
			if f.Description != "" {
				fmt.Fprintf(&b, "COMMENT ON COLUMN %s.%s IS '%s';\n", ent.Name, f.Name, f.Description)
			}
		}

		// Indexes and unique constraints (pg_dump style: named constraints).
		for _, ap := range ent.AccessPaths {
			if ap.IsUnique {
				fmt.Fprintf(&b, "ALTER TABLE ONLY %s\n    ADD CONSTRAINT %s UNIQUE (%s);\n",
					ent.Name, ap.Name, strings.Join(ap.Columns, ", "))
			} else {
				fmt.Fprintf(&b, "CREATE INDEX %s ON %s USING btree (%s);\n",
					ap.Name, ent.Name, strings.Join(ap.Columns, ", "))
			}
		}

		b.WriteByte('\n')
	}

	if len(edges) > 0 {
		b.WriteString("--\n-- Foreign Key Constraints\n--\n\n")
		for i, e := range edges {
			fmt.Fprintf(&b, "ALTER TABLE ONLY %s\n    ADD CONSTRAINT fk_%s_%s_%d FOREIGN KEY (%s) REFERENCES %s(%s);\n\n",
				e.FromEntity, e.FromEntity, e.FromField, i, e.FromField, e.ToEntity, e.ToField)
		}
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Core compilation benchmarks
// ---------------------------------------------------------------------------

func BenchmarkCompileAll_StartupSaaS(b *testing.B) {
	export := startupSaaSExport()
	c := &Compiler{Export: export}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.CompileAll(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileAll_EnterpriseERP(b *testing.B) {
	export := enterpriseERPExport()
	c := &Compiler{Export: export}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.CompileAll(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileAll_LegacyNightmare(b *testing.B) {
	export := legacyNightmareExport()
	c := &Compiler{Export: export}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.CompileAll(); err != nil {
			b.Fatal(err)
		}
	}
}

// ---------------------------------------------------------------------------
// Subset benchmarks at realistic query sizes
// ---------------------------------------------------------------------------

func BenchmarkCompileSubset_3of30(b *testing.B) {
	export := startupSaaSExport()
	c := &Compiler{Export: export}
	subset := []string{"users", "orders", "order_items"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.CompileSubset(subset); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileSubset_8of200(b *testing.B) {
	export := enterpriseERPExport()
	c := &Compiler{Export: export}
	// Pick the first 8 table names from the fixture.
	subset := make([]string, 8)
	for i := 0; i < 8; i++ {
		subset[i] = export.Entities[i].Name
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.CompileSubset(subset); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileSubset_15of500(b *testing.B) {
	export := legacyNightmareExport()
	c := &Compiler{Export: export}
	// Pick 15 evenly spaced tables to avoid clustering bias.
	const subsetSize = 15
	step := len(export.Entities) / subsetSize
	subset := make([]string, subsetSize)
	for i := 0; i < subsetSize; i++ {
		subset[i] = export.Entities[i*step].Name
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.CompileSubset(subset); err != nil {
			b.Fatal(err)
		}
	}
}

// ---------------------------------------------------------------------------
// Lighthouse benchmarks
// ---------------------------------------------------------------------------

func BenchmarkLighthouse_30(b *testing.B) {
	export := startupSaaSExport()
	c := &Compiler{Export: export}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.CompileLighthouse(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLighthouse_200(b *testing.B) {
	export := enterpriseERPExport()
	c := &Compiler{Export: export}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.CompileLighthouse(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLighthouse_500(b *testing.B) {
	export := legacyNightmareExport()
	c := &Compiler{Export: export}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.CompileLighthouse(); err != nil {
			b.Fatal(err)
		}
	}
}

// ---------------------------------------------------------------------------
// Honest metrics — prints a table of real numbers with disclaimers
// ---------------------------------------------------------------------------

func TestHonestMetrics(t *testing.T) {
	type profile struct {
		name   string
		export *schema.CtxExport
	}
	profiles := []profile{
		{"Startup SaaS", startupSaaSExport()},
		{"Enterprise ERP", enterpriseERPExport()},
		{"Legacy Nightmare", legacyNightmareExport()},
	}

	t.Log("")
	t.Log("=== Honest Benchmark Metrics ===")
	t.Log("")
	t.Logf("%-20s | %6s | %16s | %14s | %15s | %15s | %14s",
		"Schema Profile", "Tables", "Lighthouse chars", "Full DDL chars", "Est. LH tokens", "Est. DDL tokens", "Naive DDL chars")
	t.Logf("%-20s-+-%6s-+-%16s-+-%14s-+-%15s-+-%15s-+-%14s",
		strings.Repeat("-", 20), strings.Repeat("-", 6), strings.Repeat("-", 16),
		strings.Repeat("-", 14), strings.Repeat("-", 15), strings.Repeat("-", 15),
		strings.Repeat("-", 14))

	type profileResult struct {
		name       string
		tables     int
		lhChars    int
		ddlChars   int
		lhTokens   int
		ddlTokens  int
		naiveChars int
	}
	results := make([]profileResult, 0, len(profiles))

	for _, p := range profiles {
		c := &Compiler{Export: p.export}

		lhResult, err := c.CompileLighthouse()
		if err != nil {
			t.Fatalf("%s: CompileLighthouse: %v", p.name, err)
		}

		ddlResult, err := c.CompileAll()
		if err != nil {
			t.Fatalf("%s: CompileAll: %v", p.name, err)
		}

		naiveDDL := renderNaiveDDL(p.export.Entities, p.export.Edges)

		lhTokens := len(lhResult.DSL) / charsPerTokenHeuristic
		ddlTokens := len(ddlResult.DSL) / charsPerTokenHeuristic

		r := profileResult{
			name:       p.name,
			tables:     len(p.export.Entities),
			lhChars:    len(lhResult.DSL),
			ddlChars:   len(ddlResult.DSL),
			lhTokens:   lhTokens,
			ddlTokens:  ddlTokens,
			naiveChars: len(naiveDDL),
		}
		results = append(results, r)

		t.Logf("%-20s | %6d | %16d | %14d | %15d | %15d | %14d",
			p.name, r.tables, r.lhChars, r.ddlChars, r.lhTokens, r.ddlTokens, r.naiveChars)
	}

	// Subset ratios.
	t.Log("")
	t.Log("Subset Ratios (chars):")

	subsetTests := []struct {
		name       string
		export     *schema.CtxExport
		subsetSize int
	}{
		{"3 of 30", startupSaaSExport(), 3},
		{"8 of 200", enterpriseERPExport(), 8},
		{"15 of 500", legacyNightmareExport(), 15},
	}

	for _, st := range subsetTests {
		c := &Compiler{Export: st.export}

		fullResult, err := c.CompileAll()
		if err != nil {
			t.Fatalf("%s: CompileAll: %v", st.name, err)
		}

		// Pick first N tables for the subset.
		subset := make([]string, st.subsetSize)
		for i := 0; i < st.subsetSize; i++ {
			subset[i] = st.export.Entities[i].Name
		}

		subResult, err := c.CompileSubset(subset)
		if err != nil {
			t.Fatalf("%s: CompileSubset: %v", st.name, err)
		}

		ratio := float64(len(subResult.DSL)) / float64(len(fullResult.DSL)) * 100.0
		t.Logf("  %-12s %5.1f%% of full DDL (%d / %d chars)", st.name+":", ratio, len(subResult.DSL), len(fullResult.DSL))
	}

	// Naive DDL comparison.
	t.Log("")
	t.Log("dbdense DDL vs Naive (pg_dump-style) DDL:")
	for _, r := range results {
		if r.naiveChars > 0 {
			ratio := float64(r.ddlChars) / float64(r.naiveChars) * 100.0
			saving := 100.0 - ratio
			t.Logf("  %-20s dbdense=%d chars, naive=%d chars (dbdense is %.1f%% of naive, %.1f%% saving)",
				r.name+":", r.ddlChars, r.naiveChars, ratio, saving)
		}
	}

	// Disclaimers.
	t.Log("")
	t.Log("DISCLAIMERS:")
	t.Logf("  1. Token estimates use charsPerToken=%d, which is ~15-25%% optimistic for DDL.", charsPerTokenHeuristic)
	t.Logf("     Multiply estimated tokens by %.1f for conservative real-world estimates.", tokenBiasMultiplier)
	t.Log("  2. Subset filtering is basic WHERE-name-IN-list logic, not a unique feature.")
	t.Log("     Any system that knows table names can do the same filtering.")
	t.Log("  3. The 'naive DDL' baseline is a simplified pg_dump approximation.")
	t.Log("     Real pg_dump output may be larger (sequences, grants, extensions, etc.).")
	t.Log("  4. Lighthouse compactness comes from omitting column details entirely.")
	t.Log("     This is a tradeoff: compact but information-lossy.")
	t.Log("  5. These are synthetic fixtures. Real schema complexity varies widely.")
}

// ---------------------------------------------------------------------------
// Adversarial tests — scenarios designed to stress/break the compiler
// ---------------------------------------------------------------------------

func TestAdversarial_LongTableNames(t *testing.T) {
	// Tables with 60-character names. Tests that the compiler handles
	// identifiers well beyond typical lengths.
	const (
		tableCount = 20
		nameLength = 60
	)

	entities := make([]schema.Entity, 0, tableCount)
	for i := 0; i < tableCount; i++ {
		// Pad name to exactly nameLength chars.
		base := fmt.Sprintf("very_long_table_name_for_adversarial_testing_scenario_%d", i)
		if len(base) < nameLength {
			base += strings.Repeat("x", nameLength-len(base))
		} else if len(base) > nameLength {
			base = base[:nameLength]
		}
		entities = append(entities, schema.Entity{
			Name: base,
			Type: "table",
			Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "data", Type: "jsonb"},
			},
		})
	}

	// FK between first two long-named tables.
	edges := []schema.Edge{
		{
			FromEntity: entities[1].Name,
			FromField:  "id",
			ToEntity:   entities[0].Name,
			ToField:    "id",
			Type:       "foreign_key",
		},
	}

	export := &schema.CtxExport{Version: "ctxexport.v0", Entities: entities, Edges: edges}
	c := &Compiler{Export: export}

	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll with long names: %v", err)
	}

	// Verify all long names appear in DDL.
	for _, ent := range entities {
		if !strings.Contains(result.DSL, ent.Name) {
			t.Errorf("DDL missing long table name %q", ent.Name)
		}
	}

	// Check lighthouse too.
	lhResult, err := c.CompileLighthouse()
	if err != nil {
		t.Fatalf("CompileLighthouse with long names: %v", err)
	}

	tokensPerTable := float64(len(lhResult.DSL)/charsPerTokenHeuristic) / float64(tableCount)
	t.Logf("Long names (%d chars each): lighthouse tokens/table = %.1f", nameLength, tokensPerTable)
	t.Logf("  (Higher than typical ~7-10 tokens/table because name alone is ~%d tokens)", nameLength/charsPerTokenHeuristic)
}

func TestAdversarial_HighEdgeDensity(t *testing.T) {
	// One central table with 50 FKs pointing to it. Tests compiler performance
	// and output correctness under extreme edge density.
	const (
		satelliteCount = 50
		totalTables    = satelliteCount + 1 // +1 for central table
	)

	entities := make([]schema.Entity, 0, totalTables)
	// Central "hub" table.
	entities = append(entities, schema.Entity{
		Name: "central_hub",
		Type: "table",
		Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "name", Type: "text"},
		},
	})

	edges := make([]schema.Edge, 0, satelliteCount)
	for i := 0; i < satelliteCount; i++ {
		name := fmt.Sprintf("satellite_%03d", i)
		entities = append(entities, schema.Entity{
			Name: name,
			Type: "table",
			Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "hub_id", Type: "uuid", NotNull: true},
			},
		})
		edges = append(edges, schema.Edge{
			FromEntity: name,
			FromField:  "hub_id",
			ToEntity:   "central_hub",
			ToField:    "id",
			Type:       "foreign_key",
		})
	}

	export := &schema.CtxExport{Version: "ctxexport.v0", Entities: entities, Edges: edges}
	c := &Compiler{Export: export}

	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll with %d edges: %v", satelliteCount, err)
	}

	// Verify all FKs are present.
	fkCount := strings.Count(result.DSL, "ALTER TABLE")
	if fkCount != satelliteCount {
		t.Errorf("expected %d ALTER TABLE statements, got %d", satelliteCount, fkCount)
	}

	// Check lighthouse: central_hub should join to all satellites.
	lhResult, err := c.CompileLighthouse()
	if err != nil {
		t.Fatalf("CompileLighthouse: %v", err)
	}

	// The hub should have a J: section with all satellites.
	if !strings.Contains(lhResult.DSL, "T:central_hub|J:") {
		t.Error("lighthouse missing central_hub join section")
	}

	t.Logf("High edge density (%d FKs): DDL = %d chars, lighthouse = %d chars",
		satelliteCount, len(result.DSL), len(lhResult.DSL))
}

func TestAdversarial_ManyFieldsPerTable(t *testing.T) {
	// Single table with 80 columns. Tests that the compiler handles very wide
	// tables without truncation or performance issues.
	const fieldCount = 80

	fields := make([]schema.Field, 0, fieldCount)
	fields = append(fields, schema.Field{Name: "id", Type: "uuid", IsPK: true})
	for i := 1; i < fieldCount; i++ {
		fields = append(fields, schema.Field{
			Name:    fmt.Sprintf("column_%03d", i),
			Type:    "text",
			NotNull: i%3 == 0,
			Default: func() string {
				if i%10 == 0 {
					return "'default_value'"
				}
				return ""
			}(),
		})
	}

	export := &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{Name: "wide_table", Type: "table", Description: "Table with 80 columns.", Fields: fields},
		},
	}
	c := &Compiler{Export: export}

	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll with %d fields: %v", fieldCount, err)
	}

	// Verify all fields present. Count lines with "  column_" prefix.
	columnLines := 0
	for _, line := range strings.Split(result.DSL, "\n") {
		if strings.HasPrefix(line, "  column_") {
			columnLines++
		}
	}
	// fieldCount - 1 because id is the PK, not named column_XXX.
	expectedColumns := fieldCount - 1
	if columnLines != expectedColumns {
		t.Errorf("expected %d column lines, got %d", expectedColumns, columnLines)
	}

	naiveDDL := renderNaiveDDL(export.Entities, export.Edges)
	t.Logf("Wide table (%d columns): dbdense=%d chars, naive=%d chars (%.1f%% of naive)",
		fieldCount, len(result.DSL), len(naiveDDL),
		float64(len(result.DSL))/float64(len(naiveDDL))*100.0)
	t.Logf("  This is a worst case for dbdense: per-column overhead is similar to pg_dump")
}

func TestAdversarial_DeepSubfieldNesting(t *testing.T) {
	// MongoDB-style nested documents with deeply nested subfields.
	// Tests that the compiler handles the Subfields recursive structure.
	export := &schema.CtxExport{
		Version: "ctxexport.v0",
		Entities: []schema.Entity{
			{Name: "documents", Type: "table", Description: "MongoDB-style nested docs.", Fields: []schema.Field{
				{Name: "_id", Type: "objectId", IsPK: true},
				{Name: "metadata", Type: "object", Description: "Nested metadata.", Subfields: []schema.Field{
					{Name: "author", Type: "object", Subfields: []schema.Field{
						{Name: "name", Type: "string"},
						{Name: "email", Type: "string"},
						{Name: "profile", Type: "object", Subfields: []schema.Field{
							{Name: "bio", Type: "string"},
							{Name: "social", Type: "object", Subfields: []schema.Field{
								{Name: "twitter", Type: "string"},
								{Name: "github", Type: "string"},
							}},
						}},
					}},
					{Name: "tags", Type: "array"},
					{Name: "created_at", Type: "date"},
				}},
				{Name: "content", Type: "object", Subfields: []schema.Field{
					{Name: "title", Type: "string"},
					{Name: "body", Type: "string"},
					{Name: "sections", Type: "array", Subfields: []schema.Field{
						{Name: "heading", Type: "string"},
						{Name: "paragraphs", Type: "array"},
					}},
				}},
			}},
		},
	}

	c := &Compiler{Export: export}

	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll with deep subfields: %v", err)
	}

	// DDL currently flattens subfields (does not render them).
	// Verify the top-level columns appear.
	if !strings.Contains(result.DSL, "metadata object") {
		t.Error("DDL should contain metadata column")
	}
	if !strings.Contains(result.DSL, "content object") {
		t.Error("DDL should contain content column")
	}

	// Document that subfield nesting is lost in DDL output.
	// This is an honest limitation.
	if strings.Contains(result.DSL, "author") {
		t.Error("DDL should NOT contain nested subfield names (author) — they are flattened")
	}

	t.Log("Deep subfield nesting: DDL flattens nested documents to top-level columns.")
	t.Log("  Subfield details (author.name, content.sections.heading) are lost in DDL output.")
	t.Log("  This is a known limitation for MongoDB/document-store schemas.")
	t.Logf("  DDL output: %d chars", len(result.DSL))
}

func TestAdversarial_SpecialCharNames(t *testing.T) {
	// Table and column names with characters that need quoting in SQL:
	// spaces, hyphens, dots, reserved words.
	entities := []schema.Entity{
		{Name: "my-table", Type: "table", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "my-column", Type: "text"},
			{Name: "user name", Type: "text"},
		}},
		{Name: "table.with.dots", Type: "table", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "col.with.dot", Type: "text"},
		}},
		{Name: "select", Type: "table", Fields: []schema.Field{
			{Name: "id", Type: "uuid", IsPK: true},
			{Name: "from", Type: "text"},
			{Name: "where", Type: "text"},
		}},
	}

	export := &schema.CtxExport{Version: "ctxexport.v0", Entities: entities}
	c := &Compiler{Export: export}

	result, err := c.CompileAll()
	if err != nil {
		t.Fatalf("CompileAll with special chars: %v", err)
	}

	t.Log("Special character names: compiler quotes SQL identifiers in DDL output.")
	t.Log("  This keeps the rendered context closer to executable SQL for schema-qualified names,")
	t.Log("  reserved words, and identifiers with spaces or punctuation.")

	if !strings.Contains(result.DSL, `"my-table"`) {
		t.Error(`DDL should contain quoted "my-table"`)
	}
	if !strings.Contains(result.DSL, `"my-column"`) {
		t.Error(`DDL should contain quoted "my-column"`)
	}
	if !strings.Contains(result.DSL, `"user name"`) {
		t.Error(`DDL should contain quoted "user name"`)
	}
	if !strings.Contains(result.DSL, `"table.with.dots"`) {
		t.Error(`DDL should contain quoted "table.with.dots"`)
	}
	if !strings.Contains(result.DSL, `"select"`) {
		t.Error(`DDL should contain quoted "select"`)
	}

	// Lighthouse sanitization should strip special characters.
	lhResult, err := c.CompileLighthouse()
	if err != nil {
		t.Fatalf("CompileLighthouse: %v", err)
	}

	// Lighthouse strips certain chars via sanitizeLighthouse.
	// Hyphens and spaces are NOT stripped by the current replacer.
	t.Logf("Lighthouse output:\n%s", lhResult.DSL)
	t.Logf("DDL output (%d chars):\n%s", len(result.DSL), result.DSL)
}

// ---------------------------------------------------------------------------
// Naive DDL comparison test
// ---------------------------------------------------------------------------

func TestNaiveDDLComparison(t *testing.T) {
	type profile struct {
		name   string
		export *schema.CtxExport
	}
	profiles := []profile{
		{"Startup SaaS (30 tables)", startupSaaSExport()},
		{"Enterprise ERP (200 tables)", enterpriseERPExport()},
		{"Legacy Nightmare (500 tables)", legacyNightmareExport()},
	}

	t.Log("")
	t.Log("=== dbdense DDL vs Naive DDL (pg_dump approximation) ===")
	t.Log("")
	t.Logf("%-35s | %12s | %12s | %8s | %8s",
		"Profile", "dbdense chars", "naive chars", "ratio", "saving")
	t.Logf("%-35s-+-%12s-+-%12s-+-%8s-+-%8s",
		strings.Repeat("-", 35), strings.Repeat("-", 12), strings.Repeat("-", 12),
		strings.Repeat("-", 8), strings.Repeat("-", 8))

	for _, p := range profiles {
		c := &Compiler{Export: p.export}

		ddlResult, err := c.CompileAll()
		if err != nil {
			t.Fatalf("%s: CompileAll: %v", p.name, err)
		}

		naiveDDL := renderNaiveDDL(p.export.Entities, p.export.Edges)

		ratio := float64(len(ddlResult.DSL)) / float64(len(naiveDDL)) * 100.0
		saving := 100.0 - ratio

		t.Logf("%-35s | %12d | %12d | %7.1f%% | %7.1f%%",
			p.name, len(ddlResult.DSL), len(naiveDDL), ratio, saving)
	}

	t.Log("")
	t.Log("NOTES:")
	t.Log("  - dbdense DDL omits SET statements, constraint names, COMMENT ON, and ONLY keyword")
	t.Log("  - If saving is <10%, dbdense DDL is not meaningfully more compact than pg_dump")
	t.Log("  - Real pg_dump includes additional output (grants, sequences, extensions) not modeled here")
	t.Log("  - The main value proposition of dbdense is context SELECTION (subset/lighthouse), not compression")
}

// ---------------------------------------------------------------------------
// Fixture validation — ensures test fixtures are structurally valid
// ---------------------------------------------------------------------------

func TestFixtures_Validate(t *testing.T) {
	fixtures := []struct {
		name   string
		export *schema.CtxExport
	}{
		{"Startup SaaS", startupSaaSExport()},
		{"Enterprise ERP", enterpriseERPExport()},
		{"Legacy Nightmare", legacyNightmareExport()},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			if err := f.export.Validate(); err != nil {
				t.Fatalf("Validate failed: %v", err)
			}

			// Count tables, fields, edges.
			totalFields := 0
			for _, ent := range f.export.Entities {
				totalFields += len(ent.Fields)
			}

			t.Logf("%-20s tables=%d fields=%d edges=%d avg_fields=%.1f",
				f.name,
				len(f.export.Entities),
				totalFields,
				len(f.export.Edges),
				float64(totalFields)/float64(len(f.export.Entities)),
			)
		})
	}
}

// ---------------------------------------------------------------------------
// Determinism test — ensures fixtures produce identical output across runs
// ---------------------------------------------------------------------------

func TestFixtures_Deterministic(t *testing.T) {
	// Run each fixture builder twice and verify identical output.
	builders := []struct {
		name string
		fn   func() *schema.CtxExport
	}{
		{"Enterprise ERP", enterpriseERPExport},
		{"Legacy Nightmare", legacyNightmareExport},
	}

	for _, b := range builders {
		t.Run(b.name, func(t *testing.T) {
			export1 := b.fn()
			export2 := b.fn()

			c1 := &Compiler{Export: export1}
			c2 := &Compiler{Export: export2}

			r1, err := c1.CompileAll()
			if err != nil {
				t.Fatal(err)
			}
			r2, err := c2.CompileAll()
			if err != nil {
				t.Fatal(err)
			}

			if r1.DSL != r2.DSL {
				t.Errorf("Non-deterministic output: two calls to %s produced different DDL (%d vs %d chars)",
					b.name, len(r1.DSL), len(r2.DSL))
			}
		})
	}
}
