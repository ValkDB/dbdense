# Architecture

dbdense is an offline pipeline with one canonical intermediate format and one runtime server.

`Extract -> Compile -> Serve`

## Pipeline

```text
live database
    |
    v
ctxexport.json
    |
    +--> lighthouse.txt
    |
    +--> SQL DDL
    |
    v
MCP server (local files only)
```

`ctxexport.json` is the source of truth. Extraction happens once. Everything after that reads local files only.

Beyond LLM context delivery, the `ctxexport.json` artifact is a stable, machine-readable schema snapshot that can drive other tooling: offline database linters that check naming conventions and missing indexes, schema diff tools that compare snapshots across environments, and CI gates that validate schema structure without database access.

## 1. Extract

Package: `internal/extract`

Supported backends:

- PostgreSQL
  - reads `pg_catalog`
  - supports multi-schema exports
  - emits warnings for FK targets outside the exported schema set
- MongoDB
  - samples documents to infer fields and subfields
  - uses random `$sample`, not "first N documents"
  - warns when a collection has fewer documents than the configured sample size
  - inferred refs are conservative: only exact `*_id -> collection_name` matches produce edges

Optional enrichment:

- `dbdense.yaml` sidecar
  - merged during `export`
  - adds entity and field descriptions
  - emits warnings for names that do not match the exported snapshot

Runtime characteristic:

- extraction cost depends on the source database and is intentionally paid once per schema change, not once per query

## 2. Compile

Package: `internal/compile`

Core entry points:

- `CompileAll()`
- `CompileSubset(names []string)`
- `CompileLighthouse()`

Outputs:

- lighthouse
  - text format with `T:<table>|J:<neighbors>`
  - no column detail
- DDL
  - standard `CREATE TABLE`
  - anonymous FK `ALTER TABLE ... ADD FOREIGN KEY`
  - includes PKs, NOT NULL, DEFAULTs, unique constraints, and indexes
  - quotes identifiers when needed

See the [README performance section](../README.md#performance) for current benchmark numbers.

## 3. Serve

Package: `internal/server`

The MCP server is stdio-only and reads the exported snapshot from disk.

Surface:

- Resource: `dbdense://lighthouse`
- Tool: `slice`
  - input: `tables: []string`
  - output: DDL for only those tables
  - appends warnings for nonexistent tables
  - appends notes when requested tables were already sent in the same session
- Tool: `reset`
  - clears the session dedup cache

In a preliminary agentic benchmark on a seeded 8-table Postgres database, this two-tier model improved correct answers from 4/7 (baseline, no schema context) to 6/7 (dbdense), while reducing average turns from 5.1 to 3.0. Schema context guided the model to the right tables and correct column interpretations. See the [README agentic benchmark section](../README.md#agentic-benchmark) for full numbers and caveats.

## Lighthouse, slice, and session dedup

The runtime model is two-tier:

- lighthouse is cached once when the server starts
- slice calls compile table subsets on demand
- the server tracks which table names have already been returned in the current session

That dedup is in-memory and session-local. If a model asks for `users` twice, the second call does not resend the DDL unless `reset` is called.

## Reset behavior

`reset` exists because the server is intentionally static:

- use it after a schema refresh if the client keeps the same session open
- use it if you want previously delivered tables to be sent again

It does not reload files or reconnect to a database. It only clears the per-session dedup state.

## Offline model

The main architectural choice is that `serve` does not talk to the database.

Implications:

- no DB credentials at runtime
- predictable behavior in air-gapped or locked-down environments
- schema snapshots can be committed to the repo
- stale exports stay stale until `export` is run again

If you need live schema or data access during agent execution, dbdense should sit next to a live database MCP server, not replace it.

## Extending dbdense

### Adding a new database backend

Implement the `Extractor` interface in `internal/extract`:

    type Extractor interface {
        Extract(ctx context.Context) (*schema.CtxExport, error)
        Warnings() []string
    }

Register the new backend with `extract.Register("mysql", factory)` in an `init()` function. The CLI discovers backends automatically via the registry.

### Adding a new output format

Implement the `Renderer` interface in `internal/compile`:

    type Renderer interface {
        Render(entities []schema.Entity, edges []schema.Edge) string
    }

Pass the renderer to the `Compiler` via the `Renderer` field. The default is `DDLRenderer` (standard SQL DDL).
