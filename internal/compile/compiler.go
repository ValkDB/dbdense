// Package compile renders CtxExport documents into SQL DDL output
// and lighthouse DSL for LLM context delivery.
package compile

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/valkdb/dbdense/pkg/schema"
)

// Compiler renders a CtxExport as SQL DDL (full schema) or lighthouse.v0
// (table map) output for LLM context delivery.
type Compiler struct {
	Export   *schema.CtxExport
	Renderer Renderer // optional; defaults to DDLRenderer if nil
}

// Result holds the compiled output: the subset of entities/edges and the
// rendered DDL string.
type Result struct {
	Entities []schema.Entity
	Edges    []schema.Edge
	DSL      string
}

// ErrNilExport is returned when the Compiler's Export field is nil.
var ErrNilExport = errors.New("compile: export must not be nil")

// CompileAll renders every entity and edge as SQL DDL with no filtering.
func (c *Compiler) CompileAll() (*Result, error) {
	if c.Export == nil {
		return nil, ErrNilExport
	}

	entities := c.Export.Entities
	edges := c.Export.Edges

	var dsl string
	if c.Renderer != nil {
		dsl = c.Renderer.Render(entities, edges)
	} else {
		dsl = renderDDL(entities, edges)
	}

	return &Result{
		Entities: entities,
		Edges:    edges,
		DSL:      dsl,
	}, nil
}

// CompileSubset renders only the named entities and their interconnecting
// edges as SQL DDL. Entities not in names are excluded, and edges are
// kept only when both endpoints are in the filtered set.
func (c *Compiler) CompileSubset(names []string) (*Result, error) {
	if c.Export == nil {
		return nil, ErrNilExport
	}

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	filteredEntities := make([]schema.Entity, 0, len(names))
	for _, ent := range c.Export.Entities {
		if nameSet[ent.Name] {
			filteredEntities = append(filteredEntities, ent)
		}
	}

	filteredEdges := make([]schema.Edge, 0, len(c.Export.Edges))
	for _, e := range c.Export.Edges {
		if nameSet[e.FromEntity] && nameSet[e.ToEntity] {
			filteredEdges = append(filteredEdges, e)
		}
	}

	var dsl string
	if c.Renderer != nil {
		dsl = c.Renderer.Render(filteredEntities, filteredEdges)
	} else {
		dsl = renderDDL(filteredEntities, filteredEdges)
	}

	return &Result{
		Entities: filteredEntities,
		Edges:    filteredEdges,
		DSL:      dsl,
	}, nil
}

// WriteDDL renders SQL DDL to the given writer.
func WriteDDL(w io.Writer, entities []schema.Entity, edges []schema.Edge) error {
	_, err := fmt.Fprint(w, renderDDL(entities, edges))
	return err
}

// lighthouseReplacer strips characters that are structural delimiters in
// lighthouse.v0. Allocated once at package level to avoid per-call allocations.
var lighthouseReplacer = strings.NewReplacer(
	"|", "",
	"=", "",
	"(", "",
	")", "",
	",", " ",
	">", "",
	"\n", " ",
	":", "",
	"\r", "",
	"#", "", // a leading # would be parsed as a comment line
)

var sqlReservedWords = map[string]struct{}{
	"select":     {},
	"from":       {},
	"where":      {},
	"table":      {},
	"index":      {},
	"order":      {},
	"group":      {},
	"limit":      {},
	"offset":     {},
	"insert":     {},
	"update":     {},
	"delete":     {},
	"create":     {},
	"drop":       {},
	"alter":      {},
	"primary":    {},
	"foreign":    {},
	"key":        {},
	"references": {},
	"constraint": {},
	"unique":     {},
	"check":      {},
	"default":    {},
	"null":       {},
	"not":        {},
	"and":        {},
	"or":         {},
	"in":         {},
	"is":         {},
	"as":         {},
	"on":         {},
	"join":       {},
	"left":       {},
	"right":      {},
	"inner":      {},
	"outer":      {},
	"cross":      {},
	"having":     {},
	"between":    {},
	"like":       {},
	"exists":     {},
	"case":       {},
	"when":       {},
	"then":       {},
	"else":       {},
	"end":        {},
	"with":       {},
	"grant":      {},
	"revoke":     {},
	"column":     {},
	"type":       {},
	"user":       {},
	"role":       {},
	"view":       {},
	"trigger":    {},
	"function":   {},
	"schema":     {},
	"value":      {},
	"values":     {},
	"date":       {},
	"time":       {},
	"timestamp":  {},
	"row":        {},
	"rows":       {},
	"level":      {},
	"size":       {},
	"name":       {},
	"status":     {},
	"desc":       {},
	"asc":        {},
	"into":       {},
	"by":         {},
	"for":        {},
	"to":         {},
	"do":         {},
	"set":        {},
	"add":        {},
	"all":        {},
	"any":        {},
	"true":       {},
	"false":      {},
	"only":       {},
	"int":        {},
	"integer":    {},
	"float":      {},
	"real":       {},
	"double":     {},
	"boolean":    {},
	"character":  {},
	"natural":    {},
	"full":       {},
	"union":      {},
	"intersect":  {},
	"except":     {},
	"distinct":   {},
	"fetch":      {},
	"current":    {},
	"replace":    {},
	"cascade":    {},
	"restrict":   {},
	"action":     {},
	"returning":  {},
	"recursive":  {},
	"lateral":    {},
	"array":      {},
	"collate":    {},
	"over":       {},
	"partition":  {},
	"window":     {},
}

// sanitizeLighthouse strips characters that are structural delimiters in
// lighthouse.v0.
func sanitizeLighthouse(s string) string {
	return lighthouseReplacer.Replace(s)
}

func quoteIdentifier(name string) string {
	if name == "" {
		return `""`
	}

	if _, reserved := sqlReservedWords[strings.ToLower(name)]; reserved {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}

	for i, r := range name {
		if i == 0 && r >= '0' && r <= '9' {
			return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		}
		if r >= 'A' && r <= 'Z' {
			return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		}
		isAlpha := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if !isAlpha && !isDigit && r != '_' {
			return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		}
	}

	return name
}

func quoteQualifiedName(name string) string {
	if name == "" {
		return quoteIdentifier(name)
	}

	// dbdense schema-qualified entity names are emitted as schema.table.
	// Names with zero or multiple dots are treated as single identifiers.
	if strings.Count(name, ".") != 1 {
		return quoteIdentifier(name)
	}

	parts := strings.SplitN(name, ".", 2)
	if parts[0] == "" || parts[1] == "" {
		return quoteIdentifier(name)
	}
	return quoteIdentifier(parts[0]) + "." + quoteIdentifier(parts[1])
}

func joinQuotedIdentifiers(names []string) string {
	if len(names) == 0 {
		return ""
	}

	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = quoteIdentifier(name)
	}
	return strings.Join(quoted, ", ")
}

// renderDDL produces standard SQL DDL output.
//
// Output example:
//
//	-- dbdense schema context
//	CREATE TABLE users (
//	  id uuid PRIMARY KEY,
//	  email text -- Login email
//	);
//
//	-- Foreign keys
//	ALTER TABLE orders ADD FOREIGN KEY (user_id) REFERENCES users (id);
func renderDDL(entities []schema.Entity, edges []schema.Edge) string {
	var b strings.Builder
	b.Grow(len("-- dbdense schema context\n") + len(entities)*200 + len(edges)*80)
	b.WriteString("-- dbdense schema context\n")

	for i, ent := range entities {
		if i > 0 {
			b.WriteByte('\n')
		}

		if ent.Type == "view" {
			renderViewComment(&b, ent)
			continue
		}

		renderCreateTable(&b, ent)
	}

	if len(edges) > 0 {
		b.WriteString("\n-- Foreign keys\n")
		for _, e := range edges {
			fmt.Fprintf(&b, "ALTER TABLE %s ADD FOREIGN KEY (%s) REFERENCES %s (%s);\n",
				quoteQualifiedName(e.FromEntity),
				quoteIdentifier(e.FromField),
				quoteQualifiedName(e.ToEntity),
				quoteIdentifier(e.ToField))
		}
	}

	return b.String()
}

// renderViewComment emits a -- VIEW comment for view-type entities.
func renderViewComment(b *strings.Builder, ent schema.Entity) {
	colNames := make([]string, 0, len(ent.Fields))
	for _, f := range ent.Fields {
		colNames = append(colNames, quoteIdentifier(f.Name))
	}
	fmt.Fprintf(b, "-- VIEW: %s (%s)\n", quoteQualifiedName(ent.Name), strings.Join(colNames, ", "))
}

// renderCreateTable emits a CREATE TABLE statement for the given entity.
func renderCreateTable(b *strings.Builder, ent schema.Entity) {
	fmt.Fprintf(b, "CREATE TABLE %s (", quoteQualifiedName(ent.Name))
	if ent.Description != "" {
		fmt.Fprintf(b, " -- %s", ent.Description)
	}
	b.WriteByte('\n')

	// Separate PKs from regular fields to detect composite PKs.
	pks := make([]string, 0, 2) // composite PKs are rare
	for _, f := range ent.Fields {
		if f.IsPK {
			pks = append(pks, f.Name)
		}
	}

	isSinglePK := len(pks) == 1
	isCompositePK := len(pks) > 1

	for fi, f := range ent.Fields {
		isLast := fi == len(ent.Fields)-1 && !isCompositePK

		b.WriteString("  ")
		b.WriteString(quoteIdentifier(f.Name))

		if f.Type != "" {
			b.WriteByte(' ')
			b.WriteString(f.Type)
		}

		if f.IsPK && isSinglePK {
			b.WriteString(" PRIMARY KEY")
		}

		if f.NotNull && !(f.IsPK && isSinglePK) {
			// PRIMARY KEY implies NOT NULL, so skip the redundant annotation.
			b.WriteString(" NOT NULL")
		}

		if f.Default != "" {
			fmt.Fprintf(b, " DEFAULT %s", f.Default)
		}

		if !isLast {
			b.WriteByte(',')
		}

		if f.Description != "" {
			fmt.Fprintf(b, " -- %s", f.Description)
		}

		b.WriteByte('\n')
	}

	if isCompositePK {
		fmt.Fprintf(b, "  PRIMARY KEY (%s)\n", joinQuotedIdentifiers(pks))
	}

	b.WriteString(");\n")

	// Emit access paths: UNIQUE constraints and indexes.
	renderAccessPaths(b, ent)
}

// renderAccessPaths emits UNIQUE constraints and CREATE INDEX statements
// for the entity's access paths. Called immediately after the CREATE TABLE
// closing ");" so they are visually grouped with the table definition.
func renderAccessPaths(b *strings.Builder, ent schema.Entity) {
	for _, ap := range ent.AccessPaths {
		if ap.IsUnique {
			fmt.Fprintf(b, "ALTER TABLE %s ADD UNIQUE (%s);\n",
				quoteQualifiedName(ent.Name), joinQuotedIdentifiers(ap.Columns))
		} else {
			fmt.Fprintf(b, "CREATE INDEX %s ON %s (%s);\n",
				quoteIdentifier(ap.Name),
				quoteQualifiedName(ent.Name),
				joinQuotedIdentifiers(ap.Columns))
		}
	}
}

// CompileLighthouse renders a lightweight table map (lighthouse.v0) that
// lists every entity with its FK join targets but no column detail. This
// is designed to stay permanently in the LLM system prompt as a "broad
// awareness" layer while the full DDL provides depth on demand.
func (c *Compiler) CompileLighthouse() (*Result, error) {
	if c.Export == nil {
		return nil, ErrNilExport
	}

	g := BuildGraph(c.Export.Edges)
	dsl := renderLighthouse(c.Export.Entities, g)

	return &Result{
		Entities: c.Export.Entities,
		Edges:    c.Export.Edges,
		DSL:      dsl,
	}, nil
}

// WriteLighthouse renders the lighthouse.v0 DSL to the given writer.
func WriteLighthouse(w io.Writer, entities []schema.Entity, edges []schema.Edge) error {
	g := BuildGraph(edges)
	_, err := fmt.Fprint(w, renderLighthouse(entities, g))
	return err
}

// renderLighthouse produces the lighthouse.v0 text format.
//
// Format:
//
//	# lighthouse.v0
//	T:users|J:orders,sessions,payments
//	T:audit_log
func renderLighthouse(entities []schema.Entity, g *Graph) string {
	var b strings.Builder
	b.Grow(len("# lighthouse.v0\n") + len("# Table map. T=table, J=joined tables. Use slice tool for column details.\n") + len(entities)*40)
	b.WriteString("# lighthouse.v0\n")
	b.WriteString("# Table map. T=table, J=joined tables. Use slice tool for column details.\n")

	for _, ent := range entities {
		b.WriteString("T:")
		b.WriteString(sanitizeLighthouse(ent.Name))

		neighbors := g.Neighbors(ent.Name)
		if len(neighbors) > 0 {
			sanitized := make([]string, len(neighbors))
			for i, n := range neighbors {
				sanitized[i] = sanitizeLighthouse(n)
			}
			b.WriteString("|J:")
			b.WriteString(strings.Join(sanitized, ","))
		}

		b.WriteByte('\n')
	}

	return b.String()
}
