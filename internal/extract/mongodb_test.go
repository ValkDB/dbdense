package extract

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestBsonTypeName(t *testing.T) {
	tests := []struct {
		input byte
		want  string
	}{
		{0x01, "double"},
		{0x02, "string"},
		{0x03, "object"},
		{0x04, "array"},
		{0x05, "binary"},
		{0x06, "undefined"},
		{0x07, "objectId"},
		{0x08, "boolean"},
		{0x09, "datetime"},
		{0x0A, "null"},
		{0x0B, "regex"},
		{0x10, "int32"},
		{0x11, "timestamp"},
		{0x12, "int64"},
		{0x13, "decimal128"},
		{0x7F, "maxKey"},
		{0xFE, "unknown"},
		{0xFF, "minKey"},
	}
	for _, tt := range tests {
		got := bsonTypeName(tt.input)
		if got != tt.want {
			t.Errorf("bsonTypeName(0x%02X) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRefBase(t *testing.T) {
	tests := []struct {
		field  string
		want   string
		wantOK bool
	}{
		{"user_id", "user", true},
		{"line_item_id", "line_item", true},
		{"_id", "", false},
		{"userId", "", false},
		{"name", "", false},
	}

	for _, tt := range tests {
		got, ok := refBase(tt.field)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("refBase(%q) = (%q, %v), want (%q, %v)", tt.field, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestInferSubfields(t *testing.T) {
	// Build two BSON documents with overlapping fields.
	doc1, err := bson.Marshal(bson.D{
		{Key: "status", Value: "pending"},
		{Key: "channel", Value: "web"},
		{Key: "priority", Value: int32(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc2, err := bson.Marshal(bson.D{
		{Key: "status", Value: "shipped"},
		{Key: "channel", Value: "mobile"},
		{Key: "flagged", Value: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	fields := inferSubfields([]bson.Raw{bson.Raw(doc1), bson.Raw(doc2)})

	fieldMap := make(map[string]string, len(fields))
	for _, f := range fields {
		fieldMap[f.Name] = f.Type
	}

	// Union of all fields should be present.
	if fieldMap["status"] != "string" {
		t.Errorf("status: got %q, want %q", fieldMap["status"], "string")
	}
	if fieldMap["channel"] != "string" {
		t.Errorf("channel: got %q, want %q", fieldMap["channel"], "string")
	}
	if fieldMap["priority"] != "int32" {
		t.Errorf("priority: got %q, want %q", fieldMap["priority"], "int32")
	}
	if fieldMap["flagged"] != "boolean" {
		t.Errorf("flagged: got %q, want %q", fieldMap["flagged"], "boolean")
	}

	// Fields should be sorted alphabetically.
	for i := 1; i < len(fields); i++ {
		if fields[i].Name < fields[i-1].Name {
			t.Errorf("subfields not sorted: %q before %q", fields[i-1].Name, fields[i].Name)
		}
	}

	// Subfields should not have their own subfields (one level only).
	for _, f := range fields {
		if len(f.Subfields) > 0 {
			t.Errorf("subfield %q should not have nested subfields", f.Name)
		}
	}
}

func TestInferSubfields_Empty(t *testing.T) {
	fields := inferSubfields(nil)
	if len(fields) != 0 {
		t.Errorf("expected 0 subfields from nil docs, got %d", len(fields))
	}
}

func TestParseJSONSchemaValidator(t *testing.T) {
	raw, err := bson.Marshal(bson.D{
		{Key: "$jsonSchema", Value: bson.D{
			{Key: "required", Value: bson.A{"name", "email"}},
			{Key: "properties", Value: bson.D{
				{Key: "name", Value: bson.D{
					{Key: "bsonType", Value: "string"},
					{Key: "description", Value: "User's full name"},
				}},
				{Key: "email", Value: bson.D{
					{Key: "bsonType", Value: "string"},
				}},
				{Key: "age", Value: bson.D{
					{Key: "bsonType", Value: "int"},
				}},
				{Key: "address", Value: bson.D{
					{Key: "bsonType", Value: "object"},
					{Key: "properties", Value: bson.D{
						{Key: "street", Value: bson.D{{Key: "bsonType", Value: "string"}}},
						{Key: "city", Value: bson.D{{Key: "bsonType", Value: "string"}}},
					}},
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	fields := parseJSONSchemaValidator(bson.Raw(raw))
	if len(fields) != 4 {
		t.Fatalf("expected 4 fields, got %d: %+v", len(fields), fields)
	}

	fieldMap := make(map[string]struct {
		typ     string
		notNull bool
		desc    string
		subs    int
	}, len(fields))
	for _, f := range fields {
		fieldMap[f.Name] = struct {
			typ     string
			notNull bool
			desc    string
			subs    int
		}{f.Type, f.NotNull, f.Description, len(f.Subfields)}
	}

	if f := fieldMap["name"]; f.typ != "string" || !f.notNull || f.desc != "User's full name" {
		t.Errorf("name: got type=%q notNull=%v desc=%q", f.typ, f.notNull, f.desc)
	}
	if f := fieldMap["email"]; f.typ != "string" || !f.notNull {
		t.Errorf("email: got type=%q notNull=%v", f.typ, f.notNull)
	}
	if f := fieldMap["age"]; f.typ != "int" || f.notNull {
		t.Errorf("age: got type=%q notNull=%v", f.typ, f.notNull)
	}
	if f := fieldMap["address"]; f.typ != "object" || f.subs != 2 {
		t.Errorf("address: got type=%q subfields=%d", f.typ, f.subs)
	}
}

func TestParseJSONSchemaValidator_NoSchema(t *testing.T) {
	raw, err := bson.Marshal(bson.D{
		{Key: "someOtherValidator", Value: "value"},
	})
	if err != nil {
		t.Fatal(err)
	}

	fields := parseJSONSchemaValidator(bson.Raw(raw))
	if len(fields) != 0 {
		t.Errorf("expected 0 fields for non-jsonSchema validator, got %d", len(fields))
	}
}
