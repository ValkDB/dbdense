// Package gen generates synthetic ctxexport.json files for stress testing.
// It creates a configurable number of "signal" tables (that match benchmark
// scenarios) surrounded by noise tables to test context delivery at scale.
//
// Noise table realism features:
//   - ~40% of noise tables have descriptions (matching enterprise schema patterns)
//   - Field counts range from 5-25 (realistic for enterprise tables)
//   - ~60% of non-PK fields have NOT NULL constraints
//   - ~20% of fields have DEFAULT values (now(), true, 0, ”)
//   - 1-2 indexes per noise table with realistic naming (idx_tablename_column)
//   - ~5% of tables use composite PKs (junction table pattern)
//
// Remaining limitations (vs real enterprise schemas):
//   - Noise fields have random types that don't always match their names
//   - Low edge density (~30% of noise tables have FKs; real schemas are denser)
//
// For publishable benchmark results, use a real schema or a hand-designed test
// schema (see benchmark/TODO.md Phase 1).
package gen

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/valkdb/dbdense/pkg/schema"
)

const (
	// noiseNameMaxRetries is the maximum number of attempts to generate a
	// unique noise table name before falling back to a simple indexed name.
	noiseNameMaxRetries = 100

	// noiseEdgeProbability is the chance that a noise table gets a random FK
	// to another noise table, producing realistic graph density.
	noiseEdgeProbability = 0.3

	// noiseFieldMin is the minimum number of fields per noise table.
	noiseFieldMin = 5
	// noiseFieldRange is the number of additional fields added via rng.Intn,
	// so total fields per noise table is noiseFieldMin to noiseFieldMin+noiseFieldRange-1.
	noiseFieldRange = 21

	// noiseDescriptionProbability is the chance that a noise table gets a
	// human-readable description (~40%, matching enterprise schema patterns).
	noiseDescriptionProbability = 0.4

	// noiseNotNullProbability is the chance that a non-PK field gets a NOT NULL
	// constraint (~60%, common in real schemas).
	noiseNotNullProbability = 0.6

	// noiseDefaultProbability is the chance that a field gets a DEFAULT value
	// (~20%, common patterns: now(), true, 0, '').
	noiseDefaultProbability = 0.2

	// noiseCompositePKProbability is the chance that a noise table uses a
	// composite PK instead of a single id column (~5%, junction tables).
	noiseCompositePKProbability = 0.05

	// noiseMinIndexes is the minimum number of indexes per noise table.
	noiseMinIndexes = 1
	// noiseMaxIndexes is the maximum number of indexes per noise table.
	noiseMaxIndexes = 2

	// noiseUniqueIndexProbability is the chance that a generated noise
	// index is marked as unique (~25%).
	noiseUniqueIndexProbability = 0.25

	// defaultSeedMultiplier is a prime used to spread default seed values so
	// that configs with the same TotalTables but different signal tables
	// produce different random sequences.
	defaultSeedMultiplier = 1009
)

// Config controls the synthetic schema generation.
type Config struct {
	// TotalTables is the total number of tables to generate.
	TotalTables int
	// SignalTables are the tables that benchmark scenarios actually need.
	// These are always included with realistic fields and FKs.
	SignalTables []SignalTable
	// Seed for deterministic output. 0 means use TotalTables as seed.
	Seed int64
}

// SignalTable defines a "real" table that benchmark scenarios will query.
type SignalTable struct {
	Name        string
	Description string
	Fields      []schema.Field
}

// DefaultSignalTables returns the standard set of signal tables matching
// the q4_stress benchmark scenario.
func DefaultSignalTables() []SignalTable {
	return []SignalTable{
		{
			Name:        "users",
			Description: "Core identity table.",
			Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "email", Type: "text", Description: "Login email."},
				{Name: "name", Type: "text"},
				{Name: "tier", Type: "text", Description: "Subscription tier: free, premium, enterprise."},
				{Name: "region", Type: "text", Description: "Geographic region."},
				{Name: "created_at", Type: "timestamp"},
			},
		},
		{
			Name:        "orders",
			Description: "Customer orders.",
			Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "user_id", Type: "uuid", Description: "FK to users."},
				{Name: "status", Type: "text", Description: "Order lifecycle: pending, confirmed, shipped, delivered."},
				{Name: "total", Type: "numeric"},
				{Name: "payload", Type: "jsonb", Description: "JSONB. Query with payload->>'status'."},
				{Name: "created_at", Type: "timestamp"},
			},
		},
		{
			Name:        "order_items",
			Description: "Line items in an order.",
			Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "order_id", Type: "uuid", Description: "FK to orders."},
				{Name: "product_id", Type: "uuid", Description: "FK to products."},
				{Name: "quantity", Type: "int"},
				{Name: "unit_price", Type: "numeric"},
			},
		},
		{
			Name:        "products",
			Description: "Product catalog.",
			Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "name", Type: "text"},
				{Name: "sku", Type: "text"},
				{Name: "price", Type: "numeric"},
				{Name: "category", Type: "text"},
			},
		},
		{
			Name:        "payments",
			Description: "Payment transactions.",
			Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "order_id", Type: "uuid", Description: "FK to orders."},
				{Name: "amount", Type: "numeric"},
				{Name: "provider", Type: "text", Description: "Payment provider: stripe, paypal, braintree."},
				{Name: "status", Type: "text", Description: "Payment status: pending, succeeded, failed."},
				{Name: "gateway_payload", Type: "jsonb"},
				{Name: "created_at", Type: "timestamp"},
			},
		},
		{
			Name:        "shipments",
			Description: "Shipment tracking.",
			Fields: []schema.Field{
				{Name: "id", Type: "uuid", IsPK: true},
				{Name: "order_id", Type: "uuid", Description: "FK to orders."},
				{Name: "carrier", Type: "text"},
				{Name: "region", Type: "text", Description: "Destination region."},
				{Name: "status", Type: "text", Description: "Shipment status: pending, in_transit, delivered, late."},
				{Name: "shipped_at", Type: "timestamp"},
				{Name: "delivered_at", Type: "timestamp"},
				{Name: "tracking_events", Type: "jsonb"},
			},
		},
		{
			Name:        "audit_events",
			Description: "System audit trail.",
			Fields: []schema.Field{
				{Name: "id", Type: "bigint", IsPK: true},
				{Name: "actor_id", Type: "uuid"},
				{Name: "action", Type: "text"},
				{Name: "target_type", Type: "text"},
				{Name: "target_id", Type: "uuid"},
				{Name: "created_at", Type: "timestamp"},
			},
		},
		{
			Name:        "internal_metrics",
			Description: "Internal system metrics.",
			Fields: []schema.Field{
				{Name: "id", Type: "bigint", IsPK: true},
				{Name: "metric_name", Type: "text"},
				{Name: "metric_value", Type: "numeric"},
				{Name: "recorded_at", Type: "timestamp"},
			},
		},
	}
}

// signalEdges returns the FK edges between signal tables.
func signalEdges() []schema.Edge {
	return []schema.Edge{
		{FromEntity: "orders", FromField: "user_id", ToEntity: "users", ToField: "id", Type: "foreign_key"},
		{FromEntity: "order_items", FromField: "order_id", ToEntity: "orders", ToField: "id", Type: "foreign_key"},
		{FromEntity: "order_items", FromField: "product_id", ToEntity: "products", ToField: "id", Type: "foreign_key"},
		{FromEntity: "payments", FromField: "order_id", ToEntity: "orders", ToField: "id", Type: "foreign_key"},
		{FromEntity: "shipments", FromField: "order_id", ToEntity: "orders", ToField: "id", Type: "foreign_key"},
	}
}

// noise table name prefixes and suffixes for realistic-looking names.
var (
	noisePrefixes = []string{
		"tmp", "stg", "raw", "dim", "fact", "ref", "log", "hist",
		"ext", "bak", "arch", "sync", "rpt", "agg", "cache", "queue",
		"config", "meta", "stat", "tag",
	}
	noiseSuffixes = []string{
		"records", "entries", "data", "items", "events", "snapshots",
		"mappings", "lookups", "configs", "flags", "counters", "scores",
		"labels", "batches", "sessions", "tokens", "rules", "policies",
		"segments", "buckets",
	}
	noiseColumnNames = []string{
		"id", "created_at", "updated_at", "name", "value", "status",
		"type", "key", "code", "description", "is_active", "ref_id",
		"parent_id", "sequence", "hash", "payload",
	}
	noiseColumnTypes = []string{
		"uuid", "text", "int", "bigint", "boolean", "timestamp",
		"numeric", "jsonb",
	}
	// noiseDescriptions are realistic table descriptions for noise tables.
	noiseDescriptions = []string{
		"Staging data for ETL pipeline.",
		"Historical snapshots for auditing.",
		"Temporary processing table.",
		"Reference data lookup.",
		"Aggregated metrics for reporting.",
		"Configuration settings.",
		"Event log for tracking changes.",
		"Cache table for query optimization.",
		"Queue for async processing.",
		"Mapping table for external integrations.",
	}
	// noiseDefaults are common DEFAULT value patterns for various column types.
	noiseDefaults = map[string][]string{
		"timestamp": {"now()"},
		"boolean":   {"true", "false"},
		"int":       {"0"},
		"bigint":    {"0"},
		"numeric":   {"0"},
		"text":      {"''"},
		"jsonb":     {"'{}'::jsonb"},
		"uuid":      {},
	}
)

// Generate creates a synthetic CtxExport with the configured number of tables.
func Generate(cfg Config) *schema.CtxExport {
	seed := cfg.Seed
	if seed == 0 {
		seed = int64(cfg.TotalTables)*defaultSeedMultiplier + int64(len(cfg.SignalTables))
	}
	rng := rand.New(rand.NewSource(seed))

	signalCount := len(cfg.SignalTables)
	noiseCount := cfg.TotalTables - signalCount
	if noiseCount < 0 {
		noiseCount = 0
	}

	entities := make([]schema.Entity, 0, cfg.TotalTables)

	// Add signal tables first.
	for _, st := range cfg.SignalTables {
		entities = append(entities, schema.Entity{
			Name:        st.Name,
			Type:        "table",
			Description: st.Description,
			Fields:      st.Fields,
		})
	}

	// Generate noise tables.
	signalNames := make(map[string]bool, signalCount)
	for _, st := range cfg.SignalTables {
		signalNames[st.Name] = true
	}

	noiseTables := make([]schema.Entity, 0, noiseCount)
	usedNames := make(map[string]bool, cfg.TotalTables)
	for k := range signalNames {
		usedNames[k] = true
	}

	for i := 0; i < noiseCount; i++ {
		name := generateNoiseName(rng, i, usedNames)
		usedNames[name] = true

		isCompositePK := rng.Float64() < noiseCompositePKProbability

		fieldCount := noiseFieldMin + rng.Intn(noiseFieldRange)
		fields := generateNoiseFields(rng, fieldCount, isCompositePK)

		var description string
		if rng.Float64() < noiseDescriptionProbability {
			description = noiseDescriptions[rng.Intn(len(noiseDescriptions))]
		}

		// Generate 1-2 indexes per table with realistic naming.
		accessPaths := generateNoiseIndexes(rng, name, fields)

		ent := schema.Entity{
			Name:        name,
			Type:        "table",
			Description: description,
			Fields:      fields,
			AccessPaths: accessPaths,
		}
		entities = append(entities, ent)
		noiseTables = append(noiseTables, ent)
	}

	// Build edges: signal edges + some noise-to-noise FKs.
	edges := signalEdges()
	for i, source := range noiseTables {
		// ~30% of noise tables get an FK to another noise table.
		if rng.Float64() >= noiseEdgeProbability || i == 0 {
			continue
		}

		fromField := noiseEdgeSourceField(source.Fields)
		if fromField == "" {
			continue
		}

		targetIndex := rng.Intn(i)
		target := noiseTables[targetIndex]
		toField := noiseEdgeTargetField(target.Fields)
		if toField == "" {
			continue
		}

		edges = append(edges, schema.Edge{
			FromEntity: source.Name,
			FromField:  fromField,
			ToEntity:   target.Name,
			ToField:    toField,
			Type:       "foreign_key",
		})
	}

	// Validate edges: drop any that reference nonexistent entities or fields.
	// This can happen when noise field deduplication renames fields or when
	// composite-PK tables lack a single "id" column.
	entityFields := make(map[string]map[string]bool, len(entities))
	for _, ent := range entities {
		fields := make(map[string]bool, len(ent.Fields))
		for _, f := range ent.Fields {
			fields[f.Name] = true
		}
		entityFields[ent.Name] = fields
	}
	validEdges := make([]schema.Edge, 0, len(edges))
	for _, e := range edges {
		fromFields := entityFields[e.FromEntity]
		toFields := entityFields[e.ToEntity]
		if fromFields == nil || toFields == nil {
			continue
		}
		if !fromFields[e.FromField] || !toFields[e.ToField] {
			continue
		}
		validEdges = append(validEdges, e)
	}

	return &schema.CtxExport{
		Version:  "ctxexport.v0",
		Entities: entities,
		Edges:    validEdges,
	}
}

func noiseEdgeSourceField(fields []schema.Field) string {
	for _, preferred := range []string{"ref_id", "parent_id"} {
		for _, field := range fields {
			if field.IsPK {
				continue
			}
			if field.Name == preferred {
				return field.Name
			}
		}
	}

	for _, field := range fields {
		if field.IsPK {
			continue
		}
		if strings.HasSuffix(field.Name, "_id") {
			return field.Name
		}
	}

	for _, field := range fields {
		if field.IsPK {
			continue
		}
		switch field.Type {
		case "uuid", "bigint", "int":
			return field.Name
		}
	}

	return ""
}

func noiseEdgeTargetField(fields []schema.Field) string {
	for _, field := range fields {
		if field.IsPK && field.Name == "id" {
			return field.Name
		}
	}
	for _, field := range fields {
		if field.IsPK {
			return field.Name
		}
	}
	return ""
}

func generateNoiseName(rng *rand.Rand, index int, used map[string]bool) string {
	for attempt := 0; attempt < noiseNameMaxRetries; attempt++ {
		prefix := noisePrefixes[rng.Intn(len(noisePrefixes))]
		suffix := noiseSuffixes[rng.Intn(len(noiseSuffixes))]
		name := fmt.Sprintf("%s_%s_%d", prefix, suffix, index)
		if !used[name] {
			return name
		}
	}
	return fmt.Sprintf("noise_table_%d", index)
}

func generateNoiseFields(rng *rand.Rand, count int, compositePK bool) []schema.Field {
	fields := make([]schema.Field, 0, count)

	if compositePK {
		// Composite PK: two uuid columns forming a junction table.
		fields = append(fields,
			schema.Field{Name: "left_id", Type: "uuid", IsPK: true, NotNull: true},
			schema.Field{Name: "right_id", Type: "uuid", IsPK: true, NotNull: true},
		)
	} else {
		fields = append(fields, schema.Field{
			Name: "id", Type: "uuid", IsPK: true, NotNull: true,
		})
	}

	used := make(map[string]bool, count)
	if compositePK {
		used["left_id"] = true
		used["right_id"] = true
	} else {
		used["id"] = true
	}

	startIdx := len(fields)
	for i := startIdx; i < count; i++ {
		name := noiseColumnNames[rng.Intn(len(noiseColumnNames))]
		if used[name] {
			name = fmt.Sprintf("%s_%d", name, i)
		}
		used[name] = true
		colType := noiseColumnTypes[rng.Intn(len(noiseColumnTypes))]

		field := schema.Field{
			Name: name,
			Type: colType,
		}

		// ~60% of non-PK fields get NOT NULL.
		if rng.Float64() < noiseNotNullProbability {
			field.NotNull = true
		}

		// ~20% of fields get a DEFAULT value appropriate to their type.
		if rng.Float64() < noiseDefaultProbability {
			defaults := noiseDefaults[colType]
			if len(defaults) > 0 {
				field.Default = defaults[rng.Intn(len(defaults))]
			}
		}

		fields = append(fields, field)
	}
	return fields
}

// generateNoiseIndexes creates 1-2 realistic indexes for a noise table.
func generateNoiseIndexes(rng *rand.Rand, tableName string, fields []schema.Field) []schema.AccessPath {
	// Skip tables with too few non-PK fields to index.
	nonPKFields := make([]schema.Field, 0, len(fields))
	for _, f := range fields {
		if !f.IsPK {
			nonPKFields = append(nonPKFields, f)
		}
	}
	if len(nonPKFields) == 0 {
		return nil
	}

	indexCount := noiseMinIndexes + rng.Intn(noiseMaxIndexes-noiseMinIndexes+1)
	if indexCount > len(nonPKFields) {
		indexCount = len(nonPKFields)
	}

	paths := make([]schema.AccessPath, 0, indexCount)
	usedCols := make(map[string]bool, indexCount)

	for i := 0; i < indexCount; i++ {
		// Pick a random non-PK field that hasn't been indexed yet.
		var col schema.Field
		found := false
		for attempt := 0; attempt < len(nonPKFields); attempt++ {
			candidate := nonPKFields[rng.Intn(len(nonPKFields))]
			if !usedCols[candidate.Name] {
				col = candidate
				usedCols[col.Name] = true
				found = true
				break
			}
		}
		if !found {
			break
		}

		isUnique := rng.Float64() < noiseUniqueIndexProbability

		paths = append(paths, schema.AccessPath{
			Name:     fmt.Sprintf("idx_%s_%s", tableName, col.Name),
			Columns:  []string{col.Name},
			IsUnique: isUnique,
		})
	}

	return paths
}
