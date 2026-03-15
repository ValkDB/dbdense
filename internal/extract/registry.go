package extract

import "sort"

// Factory creates a new Extractor instance.
type Factory func() Extractor

// registry holds all registered extractor factories, keyed by driver name.
var registry = make(map[string]Factory, 4) // postgres, pg, mongodb, mongo

// Register adds a named extractor factory to the global registry.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// New looks up a registered factory by name, creates a new Extractor, and
// returns it. The boolean is false when no factory is registered for name.
func New(name string) (Extractor, bool) {
	f, ok := registry[name]
	if !ok {
		return nil, false
	}
	return f(), true
}

// Available returns a sorted list of all registered extractor names.
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
