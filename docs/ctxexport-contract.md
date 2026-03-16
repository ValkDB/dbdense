# ctxexport.json Contract

The `ctxexport.json` file is the canonical intermediate representation in the
dbdense pipeline. It sits between extraction (reading a live database) and
compilation (producing SQL DDL that LLM agents consume).

```
Database  -->  ctxexport.json  -->  SQL DDL  -->  LLM Agent
  (extract)      (contract)       (compile)
```

Every extractor (Postgres, MongoDB, future backends) produces one of these.
Every consumer (compiler, MCP server) reads one. The contract is the
single point of truth for what the pipeline knows about a database.

## Schema Structure

The contract is defined as Go types in `pkg/schema/types.go` and serialized
as JSON. Version: `ctxexport.v0`.

### Top Level: CtxExport

```json
{
  "version": "ctxexport.v0",
  "entities": [ ... ],
  "edges": [ ... ]
}
```

| Field      | Type       | Required | Description                                      |
|------------|------------|----------|--------------------------------------------------|
| `version`  | string     | Yes      | Contract version. Currently `"ctxexport.v0"`.    |
| `entities` | Entity[]   | Yes      | All database objects (tables, views, collections).|
| `edges`    | Edge[]     | Yes      | All relationships between entities.              |

### Entity

An entity is a table, view, materialized view, or collection.

```json
{
  "name": "orders",
  "type": "table",
  "description": "Customer orders.",
  "fields": [ ... ],
  "access_paths": [ ... ]
}
```

| Field          | Type          | Required | Description                                                  |
|----------------|---------------|----------|--------------------------------------------------------------|
| `name`         | string        | Yes      | Unique name. Must not be empty or duplicated.                |
| `type`         | string        | Yes      | Kind of object: `"table"`, `"view"`, `"materialized_view"`, `"collection"`. |
| `description`  | string        | No       | Human-readable description (from DB comments or sidecar).    |
| `fields`       | Field[]       | Yes      | Columns or document fields.                                  |
| `access_paths` | AccessPath[]  | No       | Indexes. Omitted if the entity has none (beyond PK).         |

### Field

A field is a column in a relational table or a top-level key in a document.

```json
{
  "name": "email",
  "type": "character varying(255)",
  "is_pk": false,
  "not_null": true,
  "default": "''::character varying",
  "description": "Login email.",
  "values": ["work", "personal"]
}
```

| Field         | Type    | Required | Description                                             |
|---------------|---------|----------|---------------------------------------------------------|
| `name`        | string  | Yes      | Column or field name.                                   |
| `type`        | string  | No       | Data type (e.g., `"integer"`, `"text"`, `"objectId"`).  |
| `is_pk`       | bool    | No       | `true` if this field is part of the primary key.        |
| `not_null`    | bool    | No       | `true` if the column has a NOT NULL constraint.         |
| `default`     | string  | No       | Default expression (e.g., `"now()"`, `"'active'::text"`). |
| `description` | string  | No       | Human-readable description.                             |
| `values`      | string[] | No      | Distinct low-cardinality values from sidecar enrichment. |
| `subfields`   | Field[] | No       | Nested fields for embedded documents (MongoDB only). One level deep. |

#### Subfields (MongoDB embedded documents)

When a collection has a JSON Schema validator (`$jsonSchema`), the extractor uses
it as ground truth for field names, types, required constraints, and descriptions.
Otherwise, the MongoDB extractor samples documents and encounters embedded documents
(BSON type `0x03`), sampling sub-keys and inferring their types — the same
union-and-vote logic used for top-level fields. Subfields are stored as a nested
`Field[]` on the parent field. Only one level of nesting is captured; deeper
structures remain opaque (use the sidecar `description` to document them).

```json
{
  "name": "payload",
  "type": "object",
  "subfields": [
    { "name": "channel", "type": "string" },
    { "name": "items", "type": "array" },
    { "name": "shipping", "type": "object" },
    { "name": "status", "type": "string" }
  ]
}
```

PostgreSQL fields never have subfields — JSONB structure is described via the
sidecar `description` field.

### AccessPath

An access path is an index or similar structure. Metadata only -- no row data.

```json
{
  "name": "idx_orders_user_id",
  "columns": ["user_id"],
  "is_unique": false
}
```

| Field       | Type     | Required | Description                                    |
|-------------|----------|----------|------------------------------------------------|
| `name`      | string   | Yes      | Index name.                                    |
| `columns`   | string[] | Yes      | Ordered list of indexed columns/fields.        |
| `is_unique` | bool     | Yes      | Whether the index enforces uniqueness.         |

### Edge

An edge is a relationship between two entities.

```json
{
  "from_entity": "orders",
  "from_field": "user_id",
  "to_entity": "users",
  "to_field": "id",
  "type": "foreign_key"
}
```

| Field         | Type   | Required | Description                                                    |
|---------------|--------|----------|----------------------------------------------------------------|
| `from_entity` | string | Yes      | Source entity name. Must match an entity in the `entities` list.|
| `from_field`  | string | Yes      | Source field name.                                              |
| `to_entity`   | string | Yes      | Target entity name. Must match an entity in the `entities` list.|
| `to_field`    | string | Yes      | Target field name.                                              |
| `type`        | string | Yes      | Relationship kind: `"foreign_key"` or `"inferred_ref"`.        |

## Realistic Example

```json
{
  "version": "ctxexport.v0",
  "entities": [
    {
      "name": "users",
      "type": "table",
      "description": "Core identity table.",
      "fields": [
        { "name": "id", "type": "integer", "is_pk": true, "not_null": true },
        { "name": "email", "type": "text", "not_null": true, "description": "Login email." },
        { "name": "deleted_at", "type": "timestamp with time zone", "description": "Soft delete flag." }
      ],
      "access_paths": [
        { "name": "idx_users_email", "columns": ["email"], "is_unique": true }
      ]
    },
    {
      "name": "orders",
      "type": "table",
      "description": "Customer orders.",
      "fields": [
        { "name": "id", "type": "integer", "is_pk": true, "not_null": true },
        { "name": "user_id", "type": "integer" },
        { "name": "status", "type": "text", "not_null": true, "default": "'pending'::text", "description": "Order status." },
        { "name": "total", "type": "numeric" }
      ],
      "access_paths": [
        { "name": "idx_orders_user_id", "columns": ["user_id"], "is_unique": false }
      ]
    }
  ],
  "edges": [
    {
      "from_entity": "orders",
      "from_field": "user_id",
      "to_entity": "users",
      "to_field": "id",
      "type": "foreign_key"
    }
  ]
}
```

## Design Rationale

### Database-agnostic

The same contract represents Postgres tables, MongoDB collections, and any
future backend. Entity/Field/Edge/AccessPath are abstract enough to cover
relational and document models without backend-specific fields leaking into
the schema.

### Minimal but complete

The contract captures exactly what an LLM needs to write correct queries:
entity names, field names and types, primary keys, relationships, and index
hints. Nothing more. No row data, no statistics, no storage internals.

### Edges are explicit

Relationships are declared in the `edges` array, not inferred at compile time.
Extractors resolve foreign keys (Postgres) or infer references (MongoDB)
during extraction. The compiler treats edges as given facts and uses them
for 1-hop graph traversal without re-deriving anything.

### AccessPaths are metadata-only

Indexes are recorded by name, columns, and uniqueness. No selectivity stats,
no row counts, no storage details. This keeps the contract small and avoids
leaking operational data.

### Version field and compatibility

The exporter currently writes `version: "ctxexport.v0"`, and the loader accepts
any version string that starts with `ctxexport.`. That prefix lets tooling
distinguish ctxexport-family files from unrelated JSON, but current consumers
do not enforce a specific sub-version or fail fast on version mismatches.

## How Postgres Maps to the Contract

The `PostgresExtractor` (`internal/extract/postgres.go`) queries `pg_catalog`
directly.

| Postgres concept           | Contract concept | How                                                |
|----------------------------|------------------|----------------------------------------------------|
| Tables, views, mat. views  | Entity           | `pg_class` with `relkind IN ('r','v','m')`         |
| `relkind` value            | Entity.Type      | `'r'`->`"table"`, `'v'`->`"view"`, `'m'`->`"materialized_view"` |
| `obj_description()`        | Entity.Description | Table/view comments from `pg_description`        |
| Columns (`pg_attribute`)   | Field            | `attname`, `format_type()` for type                |
| `col_description()`        | Field.Description | Column comments from `pg_description`            |
| PK constraint (`contype='p'`) | Field.IsPK    | `pg_constraint` checked per column                 |
| `attnotnull`               | Field.NotNull    | `pg_attribute.attnotnull`                          |
| `pg_attrdef`               | Field.Default    | `pg_get_expr(d.adbin, d.adrelid)` via LEFT JOIN   |
| Non-PK indexes (`pg_index`)| AccessPath       | Index name, columns, `indisunique`                 |
| FK constraints (`contype='f'`) | Edge         | `pg_constraint` with `conkey`/`confkey` mapping    |

Multi-schema support: when multiple schemas are extracted, entity names are
schema-qualified (e.g., `"billing.invoices"`) except for `public`.

## How MongoDB Maps to the Contract

The `MongoExtractor` (`internal/extract/mongodb.go`) samples documents and
inspects indexes.

| MongoDB concept                | Contract concept | How                                          |
|--------------------------------|------------------|----------------------------------------------|
| Collections                    | Entity           | `ListCollectionNames()`, type is `"collection"` |
| Top-level document keys        | Field            | Union of keys from sampled documents (default 100) |
| Most frequent BSON type        | Field.Type       | Tallied across sample, most common wins      |
| Embedded documents (type 0x03) | Field.Subfields  | One-level recursive inference of sub-key names and types |
| `_id` field                    | Field.IsPK       | Hardcoded: `_id` is always PK               |
| Non-default indexes            | AccessPath       | `Indexes().List()`, skips `_id_`             |
| `*_id` field patterns          | Edge             | Heuristic: `user_id` -> `user` collection. Type is `"inferred_ref"` |

The MongoDB extractor does not have explicit foreign keys, so edges are
inferred by naming convention: a field like `user_id` (snake_case `*_id` only)
is checked against the known collection set. Matches produce edges with
`type: "inferred_ref"` to distinguish them from authoritative foreign keys.

## How to Add a New Backend

1. Create a new file in `internal/extract/` (e.g., `mysql.go`).

2. Define a struct that implements the `Extractor` interface:

```go
type Extractor interface {
    Extract(ctx context.Context) (*schema.CtxExport, error)
    // Warnings returns non-fatal issues discovered during extraction.
    Warnings() []string
}
```

3. Map your database's concepts to the contract:

| Your DB concept     | Map to            | Notes                                         |
|---------------------|-------------------|-----------------------------------------------|
| Tables / collections| `Entity`          | Set `Name`, `Type`, `Fields`                  |
| Columns / keys      | `Field`           | Set `Name`, `Type`, `IsPK`, `NotNull`, `Default` |
| Foreign keys / refs | `Edge`            | Set all four endpoint fields + `Type`         |
| Indexes             | `AccessPath`      | Set `Name`, `Columns`, `IsUnique`             |

4. Return a `*schema.CtxExport` with `Version: "ctxexport.v0"`.

5. Wire the new extractor via `extract.Register()` in an `init()` function (see `postgres.go` or `mongodb.go` for examples).

6. The rest of the pipeline (sidecar merge, compilation, serving) works
   unchanged because it only depends on the contract, not on any
   backend-specific code.

## Sidecar Enrichment

The sidecar file (`dbdense.yaml`) lets users overlay human-written descriptions
and low-cardinality field values onto the exported contract without modifying
the source database.

```yaml
entities:
  users:
    description: "Core identity table."
    fields:
      deleted_at:
        description: "Soft delete flag."
      status:
        values: ["active", "disabled", "pending"]
```

`MergeSidecar()` (`internal/extract/sidecar.go`) applies overrides in place:

- Only non-empty entity and field descriptions replace extracted descriptions.
- Non-empty field `values` arrays replace extracted values.
- Entity names in the sidecar that do not match any exported entity produce warnings.
- Field names in the sidecar that do not match any field in the corresponding entity produce warnings.
- Unknown fields in the YAML are rejected (`KnownFields(true)`).

Sidecar is optional. If `dbdense.yaml` does not exist, `LoadSidecar()` returns
`nil` with no error and the pipeline continues with extracted metadata only.

### Describing JSON/JSONB fields

Opaque JSON columns are a common blind spot for LLMs. The extractor sees
`jsonb` as the type but knows nothing about the structure inside. Use the
description field to document the shape so the LLM can query it correctly.

**Sidecar example:**

```yaml
entities:
  orders:
    fields:
      payload:
        description: >
          JSONB. Structure: {status: string (order state: pending|paid|shipped|delivered),
          items: [{sku: string, qty: int, price: numeric}],
          shipping: {carrier: string, tracking_id: string, shipped_at: timestamptz}}.
          Query with payload->>'status', payload->'items', payload->'shipping'->>'carrier'.
```

**What the LLM receives in DDL:**

```sql
CREATE TABLE orders (
  id integer PRIMARY KEY,
  user_id integer,
  status text,
  payload jsonb -- JSONB. Structure: {status: string ...} Query with payload->>'status' ...
);
```

**Why this works:** The description is the only free-text field that flows
through to the compiled DDL as an inline comment. By packing the JSON shape
and access patterns into the description, the LLM gets indexing hints without
any contract changes. A few practical tips:

- **Name the paths**: `payload->>'status'`, `payload->'items'` — give the
  LLM the exact accessor syntax for your DB.
- **List the keys and types**: `{status: string, items: [{sku: string}]}` —
  enough for the LLM to write correct WHERE/filter clauses.
- **Note indexes on JSON paths**: if you have a GIN index or expression index
  on a JSON path, mention it: `GIN index on payload->'items'`.
- **Same approach for MongoDB objects**: describe nested fields with dot
  notation: `address.city (string), address.zip (string)`.

**MongoDB sidecar example:**

```yaml
entities:
  orders:
    fields:
      metadata:
        description: >
          Nested object. Keys: {source: string (web|mobile|api),
          utm: {campaign: string, medium: string},
          retry_count: int}. Query with metadata.source, metadata.utm.campaign.
```

## Validation Rules

`CtxExport.Validate()` (`pkg/schema/types.go`) checks:

| Rule                                    | Error message                                  |
|-----------------------------------------|------------------------------------------------|
| `version` must not be empty             | `schema: version is required`                  |
| Entity names must not be empty          | `schema: entity name must not be empty`        |
| Entity names must be unique             | `schema: duplicate entity name "<name>"`       |
| Edge field references must not be empty | `schema: edge from "<name>" to "<name>" has empty field reference` |
| Edge `from_entity` must exist           | `schema: edge references unknown entity "<name>"` |
| Edge `to_entity` must exist             | `schema: edge references unknown entity "<name>"` |
| Field names must not be empty           | `schema: entity "<name>" has a field with empty name` |
| Field names must be unique per entity   | `schema: entity "<name>" has duplicate field "<field>"` |
| Edge `from_field` must exist in entity  | `schema: edge <from>.<field> -> <to>.<field>: field "<field>" not found in entity "<from>"` |
| Edge `to_field` must exist in entity    | `schema: edge <from>.<field> -> <to>.<field>: field "<field>" not found in entity "<to>"` |
| Access path columns must exist          | `schema: entity "<name>" access path "<path>" references unknown column "<col>"` |

Validation runs automatically when loading a file via `LoadExport()`.

## Size Limits

`MaxExportFileSize` is **100 MB** (`pkg/schema/io.go`). `LoadExport()` checks
the file size before reading and rejects files that exceed this limit. This
prevents accidental memory exhaustion from malformed or excessively large
exports (e.g., a database with tens of thousands of tables).

## How Compilation Consumes the Contract

The `Compiler` (`internal/compile/compiler.go`) reads a `CtxExport` and
produces output in two formats:

- **`CompileSubset(names)`** -- renders the requested entities as SQL-first
  schema text: `CREATE TABLE` + `ALTER TABLE FOREIGN KEY` for tables and
  materialized views, and `-- VIEW:` comments for views. Used by the MCP
  server's `slice` tool to return schema text for specific objects on demand.
- **`CompileLighthouse()`** -- renders a lightweight table map as `lighthouse.v0`.
  Used by the MCP server for the `dbdense://lighthouse` resource.

The compiler only reads the contract fields documented above. It does not
access the database or require any backend-specific knowledge. This is the
key benefit of the contract: any extractor that produces valid `ctxexport.json`
gets compilation and serving for free.

## What the LLM Actually Receives

The ctxexport.json above compiles into SQL-first schema text -- the final
artifact that an LLM agent reads. Tables compile into standard SQL DDL, while
views compile into `-- VIEW:` comments:

```sql
CREATE TABLE users (
  id integer PRIMARY KEY,
  email text NOT NULL, -- Login email.
  deleted_at timestamp with time zone -- Soft delete flag.
);
ALTER TABLE users ADD UNIQUE (email);

CREATE TABLE orders (
  id integer PRIMARY KEY,
  user_id integer,
  status text NOT NULL DEFAULT 'pending'::text, -- Order status.
  total numeric
);
CREATE INDEX idx_orders_user_id ON orders (user_id);

ALTER TABLE orders ADD FOREIGN KEY (user_id) REFERENCES users (id);
```

Standard `CREATE TABLE` statements with column comments as SQL inline comments.
NOT NULL constraints and DEFAULT expressions are included when present.
Unique constraints and indexes from access paths are emitted after each table.
Foreign keys are expressed as `ALTER TABLE ... ADD FOREIGN KEY`.

This is what the entire pipeline exists to produce. The ctxexport.json is
the intermediate contract; the SQL DDL is the deliverable.

See [Architecture](architecture.md) for details.
