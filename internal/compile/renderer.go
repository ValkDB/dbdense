package compile

import "github.com/valkdb/dbdense/pkg/schema"

// Renderer converts entities and edges into a text representation.
type Renderer interface {
	Render(entities []schema.Entity, edges []schema.Edge) string
}

// DDLRenderer produces standard SQL DDL output.
type DDLRenderer struct{}

func (DDLRenderer) Render(entities []schema.Entity, edges []schema.Edge) string {
	return renderDDL(entities, edges)
}

// LighthouseRenderer produces the lightweight table map format.
type LighthouseRenderer struct{}

func (LighthouseRenderer) Render(entities []schema.Entity, edges []schema.Edge) string {
	g := BuildGraph(edges)
	return renderLighthouse(entities, g)
}
