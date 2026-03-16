package extract

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/valkdb/dbdense/pkg/schema"
	"gopkg.in/yaml.v3"
)

// Sidecar represents the dbdense.yaml override file. Users can enrich or
// correct extracted metadata without modifying the source database.
//
// Example dbdense.yaml:
//
//	entities:
//	  users:
//	    description: "Core identity table."
//	    fields:
//	      deleted_at:
//	        description: "Soft delete flag."
type Sidecar struct {
	Entities map[string]SidecarEntity `yaml:"entities"`
}

// SidecarEntity holds overrides for a single entity.
type SidecarEntity struct {
	Description string                  `yaml:"description"`
	Fields      map[string]SidecarField `yaml:"fields"`
}

// SidecarField holds overrides for a single field.
type SidecarField struct {
	Description string   `yaml:"description"`
	Values      []string `yaml:"values"`
}

// LoadSidecar reads and parses a dbdense.yaml file. Returns a nil Sidecar
// (no error) if the file does not exist.
func LoadSidecar(path string) (*Sidecar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sidecar: %w", err)
	}
	var s Sidecar
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("parse sidecar: %w", err)
	}
	return &s, nil
}

// MergeSidecar applies sidecar overrides onto a CtxExport in place.
// Only non-empty override values replace the extracted values.
// Returns warnings for sidecar entity/field names that do not match any
// entity or field in the export.
func MergeSidecar(export *schema.CtxExport, sc *Sidecar) []string {
	if export == nil || sc == nil || len(sc.Entities) == 0 {
		return nil
	}

	var warnings []string

	for i := range export.Entities {
		ent := &export.Entities[i]
		override, ok := sc.Entities[ent.Name]
		if !ok {
			continue
		}

		if override.Description != "" {
			ent.Description = override.Description
		}

		if len(override.Fields) == 0 {
			continue
		}

		fieldSet := make(map[string]bool, len(ent.Fields))
		for j := range ent.Fields {
			fieldSet[ent.Fields[j].Name] = true
			fld := &ent.Fields[j]
			fo, ok := override.Fields[fld.Name]
			if !ok {
				continue
			}
			if fo.Description != "" {
				fld.Description = fo.Description
			}
			if len(fo.Values) > 0 {
				fld.Values = fo.Values
			}
		}

		for fieldName := range override.Fields {
			if !fieldSet[fieldName] {
				warnings = append(warnings, fmt.Sprintf("sidecar: field %q in entity %q does not match any exported field", fieldName, ent.Name))
			}
		}
	}

	// Check for sidecar entity names that don't match any exported entity.
	entitySet := make(map[string]bool, len(export.Entities))
	for _, ent := range export.Entities {
		entitySet[ent.Name] = true
	}
	for entName := range sc.Entities {
		if !entitySet[entName] {
			warnings = append(warnings, fmt.Sprintf("sidecar: entity %q does not match any exported entity", entName))
		}
	}

	return warnings
}
