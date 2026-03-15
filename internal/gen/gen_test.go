package gen

import (
	"testing"
)

func TestGenerate_DefaultConfig(t *testing.T) {
	cfg := Config{
		TotalTables:  100,
		SignalTables: DefaultSignalTables(),
	}
	export := Generate(cfg)

	if len(export.Entities) != 100 {
		t.Errorf("expected 100 entities, got %d", len(export.Entities))
	}

	// Verify signal tables are present.
	names := make(map[string]bool, len(export.Entities))
	for _, e := range export.Entities {
		names[e.Name] = true
	}
	for _, st := range DefaultSignalTables() {
		if !names[st.Name] {
			t.Errorf("signal table %q not found in generated export", st.Name)
		}
	}

	// Verify signal edges are present.
	edgeCount := 0
	for _, e := range export.Edges {
		if e.FromEntity == "orders" && e.ToEntity == "users" {
			edgeCount++
		}
	}
	if edgeCount == 0 {
		t.Error("expected signal edge orders->users")
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	cfg := Config{
		TotalTables:  50,
		SignalTables: DefaultSignalTables(),
		Seed:         42,
	}
	a := Generate(cfg)
	b := Generate(cfg)

	if len(a.Entities) != len(b.Entities) {
		t.Fatal("deterministic generation should produce same entity count")
	}

	for i := range a.Entities {
		if a.Entities[i].Name != b.Entities[i].Name {
			t.Errorf("entity %d: %q != %q", i, a.Entities[i].Name, b.Entities[i].Name)
		}
	}
}

func TestGenerate_UniqueNames(t *testing.T) {
	cfg := Config{
		TotalTables:  500,
		SignalTables: DefaultSignalTables(),
	}
	export := Generate(cfg)

	seen := make(map[string]bool, len(export.Entities))
	for _, e := range export.Entities {
		if seen[e.Name] {
			t.Errorf("duplicate entity name: %q", e.Name)
		}
		seen[e.Name] = true
	}
}

func TestGenerate_Validation(t *testing.T) {
	cfg := Config{
		TotalTables:  100,
		SignalTables: DefaultSignalTables(),
	}
	export := Generate(cfg)

	if err := export.Validate(); err != nil {
		t.Errorf("generated export should be valid: %v", err)
	}
}

func TestGenerate_StrictValidation(t *testing.T) {
	seeds := []int64{0, 1, 42, 100908}

	for _, seed := range seeds {
		cfg := Config{
			TotalTables:  100,
			SignalTables: DefaultSignalTables(),
			Seed:         seed,
		}
		export := Generate(cfg)

		if err := export.ValidateStrict(); err != nil {
			t.Fatalf("seed %d: generated export should pass strict validation: %v", seed, err)
		}
	}
}

func TestGenerate_ScaleTokenEstimate(t *testing.T) {
	sizes := []int{100, 500, 1000}
	for _, size := range sizes {
		cfg := Config{
			TotalTables:  size,
			SignalTables: DefaultSignalTables(),
		}
		export := Generate(cfg)

		if len(export.Entities) != size {
			t.Errorf("size %d: expected %d entities, got %d", size, size, len(export.Entities))
		}

		// Every entity should have at least a PK field.
		for _, e := range export.Entities {
			if len(e.Fields) == 0 {
				t.Errorf("entity %q has 0 fields", e.Name)
			}
		}
	}
}

func TestGenerate_NoiseTableRealism(t *testing.T) {
	cfg := Config{
		TotalTables:  200,
		SignalTables: DefaultSignalTables(),
		Seed:         42,
	}
	export := Generate(cfg)

	signalNames := make(map[string]bool, len(cfg.SignalTables))
	for _, st := range cfg.SignalTables {
		signalNames[st.Name] = true
	}

	var (
		noiseCount       int
		withDescription  int
		withNotNull      int
		withDefault      int
		withCompositePK  int
		withIndexes      int
		totalNoiseFields int
		minFields        = 100
		maxFields        = 0
	)

	for _, ent := range export.Entities {
		if signalNames[ent.Name] {
			continue
		}
		noiseCount++

		if ent.Description != "" {
			withDescription++
		}

		if len(ent.Fields) < minFields {
			minFields = len(ent.Fields)
		}
		if len(ent.Fields) > maxFields {
			maxFields = len(ent.Fields)
		}

		// Check for composite PK.
		pkCount := 0
		for _, f := range ent.Fields {
			if f.IsPK {
				pkCount++
			}
			if !f.IsPK {
				totalNoiseFields++
				if f.NotNull {
					withNotNull++
				}
				if f.Default != "" {
					withDefault++
				}
			}
		}
		if pkCount > 1 {
			withCompositePK++
		}

		if len(ent.AccessPaths) > 0 {
			withIndexes++
		}
	}

	// ~40% of noise tables should have descriptions (allow 25-55% range).
	descRatio := float64(withDescription) / float64(noiseCount)
	if descRatio < 0.25 || descRatio > 0.55 {
		t.Errorf("description ratio = %.2f, expected ~0.40 (range 0.25-0.55)", descRatio)
	}

	// Field count range should be 5-25 (min should be >= 5 including PK).
	if minFields < 5 {
		t.Errorf("minimum noise field count = %d, expected >= 5", minFields)
	}

	// ~60% of non-PK fields should have NOT NULL (allow 45-75% range).
	if totalNoiseFields > 0 {
		notNullRatio := float64(withNotNull) / float64(totalNoiseFields)
		if notNullRatio < 0.45 || notNullRatio > 0.75 {
			t.Errorf("NOT NULL ratio = %.2f, expected ~0.60 (range 0.45-0.75)", notNullRatio)
		}
	}

	// ~20% of fields should have DEFAULT (allow 10-30% range).
	if totalNoiseFields > 0 {
		defaultRatio := float64(withDefault) / float64(totalNoiseFields)
		if defaultRatio < 0.10 || defaultRatio > 0.30 {
			t.Errorf("DEFAULT ratio = %.2f, expected ~0.20 (range 0.10-0.30)", defaultRatio)
		}
	}

	// ~5% of tables should have composite PKs (allow 1-12% for 192 noise tables).
	compositePKRatio := float64(withCompositePK) / float64(noiseCount)
	if compositePKRatio > 0.12 {
		t.Errorf("composite PK ratio = %.2f, expected ~0.05 (range 0-0.12)", compositePKRatio)
	}

	// Most tables should have indexes.
	indexRatio := float64(withIndexes) / float64(noiseCount)
	if indexRatio < 0.80 {
		t.Errorf("index ratio = %.2f, expected >= 0.80 (most tables should have 1-2 indexes)", indexRatio)
	}

	t.Logf("Noise table realism stats (%d noise tables):", noiseCount)
	t.Logf("  Descriptions: %d (%.0f%%)", withDescription, descRatio*100)
	t.Logf("  Field count range: %d-%d", minFields, maxFields)
	t.Logf("  NOT NULL fields: %d/%d (%.0f%%)", withNotNull, totalNoiseFields, float64(withNotNull)/float64(totalNoiseFields)*100)
	t.Logf("  DEFAULT fields: %d/%d (%.0f%%)", withDefault, totalNoiseFields, float64(withDefault)/float64(totalNoiseFields)*100)
	t.Logf("  Composite PKs: %d (%.1f%%)", withCompositePK, compositePKRatio*100)
	t.Logf("  With indexes: %d (%.0f%%)", withIndexes, indexRatio*100)
}

func TestGenerate_FewerThanSignal(t *testing.T) {
	cfg := Config{
		TotalTables:  3,
		SignalTables: DefaultSignalTables(),
	}
	export := Generate(cfg)

	// When TotalTables < signal count, we still get all signal tables.
	if len(export.Entities) != len(DefaultSignalTables()) {
		t.Errorf("expected %d entities (signal only), got %d",
			len(DefaultSignalTables()), len(export.Entities))
	}

	// Signal edges should still be present even with fewer total tables.
	edgeSet := make(map[string]bool, len(export.Edges))
	for _, e := range export.Edges {
		edgeSet[e.FromEntity+"."+e.FromField+"->"+e.ToEntity] = true
	}
	expectedEdges := []string{
		"orders.user_id->users",
		"order_items.order_id->orders",
		"order_items.product_id->products",
		"payments.order_id->orders",
		"shipments.order_id->orders",
	}
	for _, want := range expectedEdges {
		if !edgeSet[want] {
			t.Errorf("missing signal edge: %s", want)
		}
	}
}
