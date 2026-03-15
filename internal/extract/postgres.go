package extract

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/valkdb/dbdense/pkg/schema"
)

// PostgresExtractor extracts schema metadata from a PostgreSQL database
// using pure Go queries against pg_catalog.
type PostgresExtractor struct {
	DSN     string
	Schemas []string // defaults to ["public"] if empty

	warnings []string // non-fatal issues discovered during extraction
}

func init() {
	Register("postgres", func() Extractor { return &PostgresExtractor{} })
	Register("pg", func() Extractor { return &PostgresExtractor{} })
}

// Warnings returns non-fatal issues discovered during extraction.
func (p *PostgresExtractor) Warnings() []string {
	return p.warnings
}

// SetDSN sets the connection string for the PostgreSQL extractor.
func (p *PostgresExtractor) SetDSN(dsn string) {
	p.DSN = dsn
}

// SetSchemas sets the schema list for the PostgreSQL extractor.
func (p *PostgresExtractor) SetSchemas(schemas []string) {
	p.Schemas = schemas
}

// schemas returns the configured schema list, defaulting to ["public"].
func (p *PostgresExtractor) schemas() []string {
	if len(p.Schemas) == 0 {
		return []string{"public"}
	}
	return p.Schemas
}

// Extract connects to PostgreSQL and returns a ctxexport.v0 metadata document.
func (p *PostgresExtractor) Extract(ctx context.Context) (*schema.CtxExport, error) {
	if p.DSN == "" {
		return nil, fmt.Errorf("postgres: DSN must not be empty")
	}

	db, err := sql.Open("pgx", p.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}

	schemas := p.schemas()
	multi := len(schemas) > 1

	entities := make([]schema.Entity, 0, len(schemas)*16) // rough estimate per schema

	for _, s := range schemas {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("extraction cancelled: %w", err)
		}

		ents, err := p.extractEntities(ctx, db, s, multi)
		if err != nil {
			return nil, fmt.Errorf("extract entities (%s): %w", s, err)
		}
		entities = append(entities, ents...)
	}

	entitySet := make(map[string]bool, len(entities))
	for _, ent := range entities {
		entitySet[ent.Name] = true
	}

	edges := make([]schema.Edge, 0, len(schemas)*8)
	for _, s := range schemas {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("extraction cancelled: %w", err)
		}

		edg, err := p.extractEdges(ctx, db, s, multi, entitySet)
		if err != nil {
			return nil, fmt.Errorf("extract edges (%s): %w", s, err)
		}
		edges = append(edges, edg...)
	}

	return &schema.CtxExport{
		Version:  "ctxexport.v0",
		Entities: entities,
		Edges:    edges,
	}, nil
}

// qualifiedName returns a schema-qualified table name when multiple schemas are in use.
func (p *PostgresExtractor) qualifiedName(schemaName, table string, multi bool) string {
	if multi && schemaName != "public" {
		return schemaName + "." + table
	}
	return table
}

// extractEntities queries tables/views in a schema and returns them as Entities.
func (p *PostgresExtractor) extractEntities(ctx context.Context, db *sql.DB, schemaName string, multi bool) ([]schema.Entity, error) {
	tables, err := p.listTables(ctx, db, schemaName, multi)
	if err != nil {
		return nil, err
	}

	entities := make([]schema.Entity, 0, len(tables))
	for _, tbl := range tables {
		fields, err := p.extractFields(ctx, db, schemaName, tbl.rawName)
		if err != nil {
			return nil, fmt.Errorf("fields for %s: %w", tbl.name, err)
		}

		paths, err := p.extractAccessPaths(ctx, db, schemaName, tbl.rawName)
		if err != nil {
			return nil, fmt.Errorf("indexes for %s: %w", tbl.name, err)
		}

		e := schema.Entity{
			Name:        tbl.name,
			Type:        tbl.kind,
			Description: tbl.description,
			Fields:      fields,
		}
		if len(paths) > 0 {
			e.AccessPaths = paths
		}
		entities = append(entities, e)
	}
	return entities, nil
}

// tableInfo holds intermediate metadata for a single table or view.
type tableInfo struct {
	name        string // possibly schema-qualified
	rawName     string // unqualified name for queries
	kind        string
	description string
}

// listTables queries pg_class for all tables, views, and materialized views in a schema.
func (p *PostgresExtractor) listTables(ctx context.Context, db *sql.DB, schemaName string, multi bool) ([]tableInfo, error) {
	const query = `
		SELECT
			c.relname,
			CASE c.relkind
				WHEN 'r' THEN 'table'
				WHEN 'v' THEN 'view'
				WHEN 'm' THEN 'materialized_view'
				ELSE 'table'
			END,
			COALESCE(obj_description(c.oid), '')
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1
		  AND c.relkind IN ('r', 'v', 'm')
		ORDER BY c.relname`

	rows, err := db.QueryContext(ctx, query, schemaName)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tables := make([]tableInfo, 0, 16) // typical schema has 10-20 tables
	for rows.Next() {
		var t tableInfo
		if err := rows.Scan(&t.rawName, &t.kind, &t.description); err != nil {
			return nil, err
		}
		t.name = p.qualifiedName(schemaName, t.rawName, multi)
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// extractFields queries pg_attribute for all columns of a table, including
// NOT NULL constraints and DEFAULT expressions.
func (p *PostgresExtractor) extractFields(ctx context.Context, db *sql.DB, schemaName, table string) ([]schema.Field, error) {
	const query = `
		SELECT
			a.attname,
			pg_catalog.format_type(a.atttypid, a.atttypmod),
			COALESCE(col_description(a.attrelid, a.attnum), ''),
			EXISTS (
				SELECT 1
				FROM pg_catalog.pg_constraint con
				WHERE con.conrelid = a.attrelid
				  AND con.contype = 'p'
				  AND a.attnum = ANY(con.conkey)
			),
			a.attnotnull,
			COALESCE(pg_catalog.pg_get_expr(d.adbin, d.adrelid), '')
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_catalog.pg_attrdef d
			ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = $1
		  AND c.relname = $2
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY a.attnum`

	rows, err := db.QueryContext(ctx, query, schemaName, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	fields := make([]schema.Field, 0, 16) // typical table has 8-20 columns
	for rows.Next() {
		var f schema.Field
		if err := rows.Scan(&f.Name, &f.Type, &f.Description, &f.IsPK, &f.NotNull, &f.Default); err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

// extractAccessPaths queries pg_index for non-primary indexes on a table.
func (p *PostgresExtractor) extractAccessPaths(ctx context.Context, db *sql.DB, schemaName, table string) ([]schema.AccessPath, error) {
	const query = `
		SELECT
			i.relname,
			array_agg(a.attname ORDER BY k.n),
			ix.indisunique
		FROM pg_catalog.pg_index ix
		JOIN pg_catalog.pg_class t ON t.oid = ix.indrelid
		JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = t.relnamespace
		CROSS JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, n)
		JOIN pg_catalog.pg_attribute a
		  ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE n.nspname = $1
		  AND t.relname = $2
		  AND NOT ix.indisprimary
		GROUP BY i.relname, ix.indisunique
		ORDER BY i.relname`

	rows, err := db.QueryContext(ctx, query, schemaName, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	paths := make([]schema.AccessPath, 0, 4) // typical table has 1-4 indexes
	for rows.Next() {
		var ap schema.AccessPath
		var cols []string
		if err := rows.Scan(&ap.Name, (*pgStringArray)(&cols), &ap.IsUnique); err != nil {
			return nil, err
		}
		ap.Columns = cols
		paths = append(paths, ap)
	}
	return paths, rows.Err()
}

// extractEdges queries pg_constraint for foreign key relationships in a schema.
// Composite (multi-column) FKs are decomposed into separate Edge objects per column
// because schema.Edge supports only single FromField/ToField. This is a known
// schema fidelity trade-off — composite FKs are rare in practice.
// Edges whose source or target entities are not present in entitySet are skipped
// with a warning so the resulting export always validates end-to-end.
func (p *PostgresExtractor) extractEdges(ctx context.Context, db *sql.DB, schemaName string, multi bool, entitySet map[string]bool) ([]schema.Edge, error) {
	const query = `
		SELECT
			src.relname,
			sa.attname,
			tgt.relname,
			ta.attname,
			tgt_ns.nspname
		FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class src ON src.oid = con.conrelid
		JOIN pg_catalog.pg_class tgt ON tgt.oid = con.confrelid
		JOIN pg_catalog.pg_namespace src_ns ON src_ns.oid = src.relnamespace
		JOIN pg_catalog.pg_namespace tgt_ns ON tgt_ns.oid = tgt.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey, con.confkey)
			WITH ORDINALITY AS cols(src_attnum, tgt_attnum, ord)
		JOIN pg_catalog.pg_attribute sa
			ON sa.attrelid = src.oid AND sa.attnum = cols.src_attnum
		JOIN pg_catalog.pg_attribute ta
			ON ta.attrelid = tgt.oid AND ta.attnum = cols.tgt_attnum
		WHERE con.contype = 'f'
		  AND src_ns.nspname = $1
		ORDER BY src.relname, cols.ord`

	rows, err := db.QueryContext(ctx, query, schemaName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	edges := make([]schema.Edge, 0, 8) // typical schema has several FKs
	for rows.Next() {
		var fromRaw, fromCol, toRaw, toCol, toSchema string
		if err := rows.Scan(&fromRaw, &fromCol, &toRaw, &toCol, &toSchema); err != nil {
			return nil, err
		}

		fromEntity := p.qualifiedName(schemaName, fromRaw, multi)
		toEntity := p.qualifiedName(toSchema, toRaw, multi)

		if edge, ok := p.classifyEdge(fromEntity, fromCol, toEntity, toCol, entitySet); ok {
			edges = append(edges, edge)
		}
	}
	return edges, rows.Err()
}

// classifyEdge checks whether both endpoints of an FK edge are present in the
// exported entity set. If so, it returns the Edge and true. If either endpoint
// is missing, it appends a warning to p.warnings and returns false.
func (p *PostgresExtractor) classifyEdge(fromEntity, fromCol, toEntity, toCol string, entitySet map[string]bool) (schema.Edge, bool) {
	missing := missingEntities(entitySet, fromEntity, toEntity)
	if len(missing) > 0 {
		p.warnings = append(p.warnings,
			fmt.Sprintf("skipping FK %s.%s -> %s.%s: endpoint entity not exported (%s)",
				fromEntity, fromCol, toEntity, toCol, strings.Join(missing, ", ")))
		return schema.Edge{}, false
	}
	return schema.Edge{
		FromEntity: fromEntity,
		FromField:  fromCol,
		ToEntity:   toEntity,
		ToField:    toCol,
		Type:       "foreign_key",
	}, true
}

func missingEntities(entitySet map[string]bool, names ...string) []string {
	missing := make([]string, 0, len(names))
	for _, name := range names {
		if !entitySet[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// pgStringArray scans a PostgreSQL text[] into a Go []string.
type pgStringArray []string

// Scan implements sql.Scanner for PostgreSQL text[] values.
func (a *pgStringArray) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*a = nil
		return nil
	case []byte:
		*a = parseTextArray(string(v))
		return nil
	case string:
		*a = parseTextArray(v)
		return nil
	default:
		return fmt.Errorf("pgStringArray: expected []byte or string, got %T", src)
	}
}

// parseTextArray parses a PostgreSQL array literal like {foo,bar,baz}.
func parseTextArray(s string) []string {
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		return nil
	}

	result := make([]string, 0, 8) // typical arrays have 1-5 elements
	var current []byte
	quoted := false
	escaped := false

	for i := 0; i < len(inner); i++ {
		ch := inner[i]
		switch {
		case escaped:
			current = append(current, ch)
			escaped = false
		case ch == '\\':
			escaped = true
		case ch == '"' && !quoted:
			quoted = true
		case ch == '"' && quoted:
			quoted = false
		case ch == ',' && !quoted:
			if elem := string(current); elem != "NULL" {
				result = append(result, elem)
			}
			current = current[:0]
		default:
			current = append(current, ch)
		}
	}
	if elem := string(current); elem != "NULL" {
		result = append(result, elem)
	}
	return result
}
