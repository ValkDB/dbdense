// Package registry maps provider IDs to provider constructors.
package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/valkdb/dbdense/benchmark/provider"
	"github.com/valkdb/dbdense/benchmark/provider/claude"
)

// Supported returns all provider IDs supported by the benchmark harness.
func Supported() []string {
	ids := []string{"claude"}
	sort.Strings(ids)
	return ids
}

// New returns a provider by ID.
func New(id string) (provider.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "claude":
		return claude.New()
	default:
		return nil, fmt.Errorf("%w: unsupported provider %q (supported: %s)", provider.ErrProviderUnavailable, id, strings.Join(Supported(), ", "))
	}
}
