// Package extract contains database metadata extractors and sidecar merging.
package extract

import (
	"context"

	"github.com/valkdb/dbdense/pkg/schema"
)

// Extractor pulls structural and semantic metadata from a database
// and returns a canonical CtxExport.
type Extractor interface {
	Extract(ctx context.Context) (*schema.CtxExport, error)
	// Warnings returns non-fatal issues discovered during extraction.
	Warnings() []string
}

// Configurable is implemented by extractors that accept a DSN and schema list
// via a uniform interface. This allows buildExtractor (and future drivers)
// to configure common settings without type-switching on concrete types.
type Configurable interface {
	SetDSN(string)
	SetSchemas([]string)
}
