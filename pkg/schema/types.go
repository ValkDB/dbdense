// Package schema defines the shared data types for the dbdense pipeline.
// These structs map directly to the ctxexport.v0 JSON contract.
package schema

import (
	"fmt"
	"strings"
)

// CtxExport is the top-level canonical export document.
type CtxExport struct {
	Version  string   `json:"version"`
	Entities []Entity `json:"entities"`
	Edges    []Edge   `json:"edges"`
}

// Validate checks the CtxExport for structural integrity.
func (e *CtxExport) Validate() error {
	if e.Version == "" {
		return fmt.Errorf("schema: version is required")
	}
	seen := make(map[string]bool, len(e.Entities))
	for _, ent := range e.Entities {
		if ent.Name == "" {
			return fmt.Errorf("schema: entity name must not be empty")
		}
		if seen[ent.Name] {
			return fmt.Errorf("schema: duplicate entity name %q", ent.Name)
		}
		seen[ent.Name] = true
	}
	for _, edge := range e.Edges {
		if edge.FromField == "" || edge.ToField == "" {
			return fmt.Errorf("schema: edge from %q to %q has empty field reference", edge.FromEntity, edge.ToEntity)
		}
		if !seen[edge.FromEntity] {
			return fmt.Errorf("schema: edge references unknown entity %q", edge.FromEntity)
		}
		if !seen[edge.ToEntity] {
			return fmt.Errorf("schema: edge references unknown entity %q", edge.ToEntity)
		}
	}
	return nil
}

// ValidateStrict runs all basic validation plus additional checks:
// duplicate fields within entities, edge field existence, and
// access path column existence.
func (e *CtxExport) ValidateStrict() error {
	if err := e.Validate(); err != nil {
		return err
	}

	// Build field lookup: entity name -> set of field names.
	entityFields := make(map[string]map[string]bool, len(e.Entities))
	for _, ent := range e.Entities {
		fields := make(map[string]bool, len(ent.Fields))
		for _, f := range ent.Fields {
			if f.Name == "" {
				return fmt.Errorf("schema: entity %q has a field with empty name", ent.Name)
			}
			if fields[f.Name] {
				return fmt.Errorf("schema: entity %q has duplicate field %q", ent.Name, f.Name)
			}
			fields[f.Name] = true
		}
		entityFields[ent.Name] = fields

		// Validate access path columns exist. Expression-based index
		// columns (containing dots, e.g. "payload.status") are skipped
		// because they reference nested paths, not top-level fields.
		for _, ap := range ent.AccessPaths {
			for _, col := range ap.Columns {
				if strings.Contains(col, ".") {
					continue
				}
				if !fields[col] {
					return fmt.Errorf("schema: entity %q access path %q references unknown column %q", ent.Name, ap.Name, col)
				}
			}
		}
	}

	// Validate edge field names exist in their entities.
	for _, edge := range e.Edges {
		fromFields := entityFields[edge.FromEntity]
		if fromFields != nil && !fromFields[edge.FromField] {
			return fmt.Errorf("schema: edge %s.%s -> %s.%s: field %q not found in entity %q",
				edge.FromEntity, edge.FromField, edge.ToEntity, edge.ToField,
				edge.FromField, edge.FromEntity)
		}
		toFields := entityFields[edge.ToEntity]
		if toFields != nil && !toFields[edge.ToField] {
			return fmt.Errorf("schema: edge %s.%s -> %s.%s: field %q not found in entity %q",
				edge.FromEntity, edge.FromField, edge.ToEntity, edge.ToField,
				edge.ToField, edge.ToEntity)
		}
	}

	return nil
}

// Entity represents a database object (table, view, etc.).
type Entity struct {
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Description string       `json:"description,omitempty"`
	Fields      []Field      `json:"fields"`
	AccessPaths []AccessPath `json:"access_paths,omitempty"`
}

// Field represents a column within an entity.
type Field struct {
	Name        string  `json:"name"`
	Type        string  `json:"type,omitempty"`
	IsPK        bool    `json:"is_pk,omitempty"`
	NotNull     bool    `json:"not_null,omitempty"`  // true if column has a NOT NULL constraint
	Default     string  `json:"default,omitempty"`   // default expression (e.g., "now()", "'active'::text")
	Description string  `json:"description,omitempty"`
	Subfields   []Field `json:"subfields,omitempty"`
}

// AccessPath represents an index or similar access structure on an entity.
type AccessPath struct {
	Name     string   `json:"name"`
	Columns  []string `json:"columns"`
	IsUnique bool     `json:"is_unique"`
}

// Edge represents a relationship (foreign key) between two entities.
type Edge struct {
	FromEntity string `json:"from_entity"`
	FromField  string `json:"from_field"`
	ToEntity   string `json:"to_entity"`
	ToField    string `json:"to_field"`
	// Type is an optional classification (e.g., "fk", "has_many"). Informational
	// only — not validated or used in compilation.
	Type string `json:"type"`
}
