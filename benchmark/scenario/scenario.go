package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// CategoryRegular is the default scenario category.
	CategoryRegular = "regular"
	// CategoryStress is used for scalability/context-limit stress scenarios.
	CategoryStress = "stress"
)

// Verification configures deterministic checks against provider response.
type Verification struct {
	Response *ResponseVerification `yaml:"response"`
}

// ResponseVerification checks required numeric fields in model response JSON.
type ResponseVerification struct {
	NumericEquals map[string]float64 `yaml:"numeric_equals"`
}

// Scenario is one benchmark prompt + expected behavior contract.
type Scenario struct {
	ID           string       `yaml:"id"`
	Label        string       `yaml:"label"`
	Category     string       `yaml:"category"`
	Prompt       string       `yaml:"prompt"`
	Verification Verification `yaml:"verification"`
}

// Validate checks basic integrity and defaults.
func (s *Scenario) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("scenario id is required")
	}
	if strings.TrimSpace(s.Label) == "" {
		return fmt.Errorf("scenario %q label is required", s.ID)
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return fmt.Errorf("scenario %q prompt is required", s.ID)
	}
	if s.Category == "" {
		s.Category = CategoryRegular
	}
	if s.Category != CategoryRegular && s.Category != CategoryStress {
		return fmt.Errorf("scenario %q has unsupported category %q", s.ID, s.Category)
	}
	if s.Verification.Response == nil {
		return fmt.Errorf("scenario %q must define verification.response", s.ID)
	}
	if len(s.Verification.Response.NumericEquals) == 0 {
		return fmt.Errorf("scenario %q must define verification.response.numeric_equals", s.ID)
	}
	for k := range s.Verification.Response.NumericEquals {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("scenario %q has empty verification.response.numeric_equals key", s.ID)
		}
	}
	return nil
}

// LoadDir reads all .yaml/.yml scenarios from dir in lexical order.
func LoadDir(dir string) ([]Scenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read scenarios dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var out []Scenario
	seen := make(map[string]bool, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		s, err := LoadFile(path)
		if err != nil {
			return nil, err
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("duplicate scenario id %q", s.ID)
		}
		seen[s.ID] = true
		out = append(out, s)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no scenario YAML files found in %s", dir)
	}
	return out, nil
}

// LoadFile reads and validates one scenario YAML.
func LoadFile(path string) (Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario %q: %w", path, err)
	}
	var s Scenario
	if err := yaml.Unmarshal(data, &s); err != nil {
		return Scenario{}, fmt.Errorf("parse scenario %q: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return Scenario{}, fmt.Errorf("scenario %q invalid: %w", path, err)
	}
	return s, nil
}
