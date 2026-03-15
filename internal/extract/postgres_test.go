package extract

import (
	"strings"
	"testing"
)

func TestParseTextArray(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"{}", nil},
		{"{foo}", []string{"foo"}},
		{"{foo,bar}", []string{"foo", "bar"}},
		{"{foo,bar,baz}", []string{"foo", "bar", "baz"}},
		{`{"foo,bar",baz}`, []string{"foo,bar", "baz"}},
		{`{"foo\"bar"}`, []string{`foo"bar`}},
		{`{"foo\\bar"}`, []string{`foo\bar`}},
		{"", nil},
		{"{", nil},
		{"invalid", nil},
	}
	for _, tt := range tests {
		got := parseTextArray(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseTextArray(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseTextArray(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestPgStringArrayScan(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    []string
		wantErr bool
	}{
		{"nil", nil, nil, false},
		{"bytes", []byte("{a,b}"), []string{"a", "b"}, false},
		{"string", "{x,y}", []string{"x", "y"}, false},
		{"invalid type", 123, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var a pgStringArray
			err := a.Scan(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Scan error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if len(a) != len(tt.want) {
					t.Fatalf("got %v, want %v", []string(a), tt.want)
				}
				for i := range a {
					if a[i] != tt.want[i] {
						t.Errorf("[%d] = %q, want %q", i, a[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestQualifiedName(t *testing.T) {
	p := &PostgresExtractor{}
	tests := []struct {
		schema string
		table  string
		multi  bool
		want   string
	}{
		{"public", "users", false, "users"},
		{"public", "users", true, "users"},
		{"auth", "sessions", true, "auth.sessions"},
		{"auth", "sessions", false, "sessions"},
	}
	for _, tt := range tests {
		got := p.qualifiedName(tt.schema, tt.table, tt.multi)
		if got != tt.want {
			t.Errorf("qualifiedName(%q, %q, %v) = %q, want %q", tt.schema, tt.table, tt.multi, got, tt.want)
		}
	}
}

func TestClassifyEdge_SkippedFK_Warning(t *testing.T) {
	// Simulates an FK edge whose target entity is outside the exported schema.
	// Verifies that classifyEdge skips the edge and appends a warning to
	// PostgresExtractor.warnings mentioning the missing endpoint.
	entitySet := map[string]bool{
		"orders": true,
		"users":  true,
	}

	tests := []struct {
		name       string
		fromEntity string
		fromCol    string
		toEntity   string
		toCol      string
		wantOK     bool
		wantWarn   string // substring expected in the warning, if any
	}{
		{
			name:       "both endpoints present",
			fromEntity: "orders",
			fromCol:    "user_id",
			toEntity:   "users",
			toCol:      "id",
			wantOK:     true,
		},
		{
			name:       "target outside exported schema",
			fromEntity: "orders",
			fromCol:    "vendor_id",
			toEntity:   "external.vendors",
			toCol:      "id",
			wantOK:     false,
			wantWarn:   "external.vendors",
		},
		{
			name:       "source outside exported schema",
			fromEntity: "billing.invoices",
			fromCol:    "order_id",
			toEntity:   "orders",
			toCol:      "id",
			wantOK:     false,
			wantWarn:   "billing.invoices",
		},
		{
			name:       "both endpoints missing",
			fromEntity: "billing.invoices",
			fromCol:    "vendor_id",
			toEntity:   "external.vendors",
			toCol:      "id",
			wantOK:     false,
			wantWarn:   "billing.invoices",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PostgresExtractor{}

			edge, ok := p.classifyEdge(tt.fromEntity, tt.fromCol, tt.toEntity, tt.toCol, entitySet)
			if ok != tt.wantOK {
				t.Fatalf("classifyEdge() ok = %v, want %v", ok, tt.wantOK)
			}

			if tt.wantOK {
				// Edge should be populated correctly.
				if edge.FromEntity != tt.fromEntity || edge.FromField != tt.fromCol ||
					edge.ToEntity != tt.toEntity || edge.ToField != tt.toCol {
					t.Errorf("edge = %+v, want from=%s.%s to=%s.%s",
						edge, tt.fromEntity, tt.fromCol, tt.toEntity, tt.toCol)
				}
				if edge.Type != "foreign_key" {
					t.Errorf("edge.Type = %q, want %q", edge.Type, "foreign_key")
				}
				if len(p.warnings) != 0 {
					t.Errorf("expected no warnings, got %v", p.warnings)
				}
			} else {
				// Warning should be appended.
				if len(p.warnings) != 1 {
					t.Fatalf("expected 1 warning, got %d: %v", len(p.warnings), p.warnings)
				}
				w := p.warnings[0]
				if !strings.Contains(w, "skipping FK") {
					t.Errorf("warning should contain 'skipping FK', got: %s", w)
				}
				if !strings.Contains(w, tt.wantWarn) {
					t.Errorf("warning should mention %q, got: %s", tt.wantWarn, w)
				}
				if !strings.Contains(w, "endpoint entity not exported") {
					t.Errorf("warning should contain 'endpoint entity not exported', got: %s", w)
				}
			}
		})
	}
}

func TestClassifyEdge_MultipleWarnings(t *testing.T) {
	// Verifies that multiple skipped edges accumulate warnings on the same extractor.
	entitySet := map[string]bool{
		"users": true,
	}

	p := &PostgresExtractor{}

	// First skipped edge.
	_, ok := p.classifyEdge("orders", "user_id", "users", "id", entitySet)
	if ok {
		t.Fatal("expected orders to be missing from entitySet")
	}

	// Second skipped edge.
	_, ok = p.classifyEdge("payments", "order_id", "orders", "id", entitySet)
	if ok {
		t.Fatal("expected both endpoints to be missing from entitySet")
	}

	// Both warnings should be present.
	if len(p.warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d: %v", len(p.warnings), p.warnings)
	}

	if !strings.Contains(p.warnings[0], "orders") {
		t.Errorf("first warning should mention 'orders', got: %s", p.warnings[0])
	}
	if !strings.Contains(p.warnings[1], "payments") {
		t.Errorf("second warning should mention 'payments', got: %s", p.warnings[1])
	}
}

func FuzzParseTextArray(f *testing.F) {
	f.Add("{}")
	f.Add("{foo,bar}")
	f.Add("{\"quoted\",value}")
	f.Add("{NULL,\"NULL\"}")
	f.Add("{a,b,c,d,e}")
	f.Fuzz(func(t *testing.T, s string) {
		parseTextArray(s) // should not panic
	})
}

func TestMissingEntities(t *testing.T) {
	entitySet := map[string]bool{
		"users":         true,
		"auth.sessions": true,
	}

	tests := []struct {
		name  string
		names []string
		want  []string
	}{
		{name: "all present", names: []string{"users", "auth.sessions"}, want: nil},
		{name: "target missing", names: []string{"users", "audit.logs"}, want: []string{"audit.logs"}},
		{name: "both missing", names: []string{"orders", "audit.logs"}, want: []string{"orders", "audit.logs"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingEntities(entitySet, tt.names...)
			if len(got) != len(tt.want) {
				t.Fatalf("missingEntities(%v) = %v, want %v", tt.names, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("missingEntities(%v)[%d] = %q, want %q", tt.names, i, got[i], tt.want[i])
				}
			}
		})
	}
}
