# dbdense - Offline schema context for LLM agents
Extract your database schema once. Serve it to AI agents via MCP without a live connection.

<img src="docs/media/demo.svg" alt="dbdense demo" width="820">

dbdense is an offline pipeline for turning a live database into local schema artifacts:

- `ctxexport.json`: a repo-committable schema snapshot
- `dbdense.yaml`: optional sidecar descriptions and hints
- `lighthouse.txt` plus on-demand DDL slices delivered over MCP

After `export`, the runtime path is local-only. `compile`, `serve`, and Claude/MCP usage do not need database credentials or a live database connection. That makes the tool usable in offline and air-gapped environments, and it keeps production credentials out of the agent runtime.

## Why this shape

Large schema dumps are not usually hard because DDL is exotic. They are hard because most questions only need a few tables, and sending everything wastes prompt space. dbdense keeps a compact table map cheap enough to keep around, then sends table-level DDL only when the agent asks for it.

## Architecture

`Extract -> Compile -> Serve`

1. `export` reads PostgreSQL or MongoDB metadata once and writes `ctxexport.json`.
2. `compile` renders either a lightweight lighthouse map or full SQL DDL from that snapshot.
3. `serve` exposes the snapshot over MCP:
   - Resource: `dbdense://lighthouse`
   - Tool: `slice`
   - Tool: `reset`

## Two-tier context model

- `lighthouse`
  - Exposed as the `dbdense://lighthouse` MCP resource; some clients may also keep it in prompt context.
  - Contains table names plus FK neighbors.
  - Cheap enough to keep around for broad schema awareness.
- `slice`
  - Returned on demand through the MCP `slice` tool.
  - Uses standard SQL DDL for only the requested tables.
  - Session dedup avoids re-sending the same tables on later turns.

Example lighthouse:

```text
# lighthouse.v0
# Table map. T=table, J=joined tables. Use slice tool for column details.
T:users|J:orders,sessions
T:orders|J:payments,shipments,users
T:audit_log
```

Example slice output:

```sql
-- dbdense schema context
CREATE TABLE users (
  id uuid PRIMARY KEY,
  email text,
  deleted_at timestamptz
);

CREATE TABLE orders (
  id uuid PRIMARY KEY,
  user_id uuid,
  status text
);

-- Foreign keys
ALTER TABLE orders ADD FOREIGN KEY (user_id) REFERENCES users (id);
```

## Performance

Numbers below come from the compile test suite and Go benchmarks in `internal/compile`. Token counts are estimates based on `chars / 4`, which the tests note is roughly 15-25% optimistic for DDL.

| Fixture | Tables | Lighthouse chars | Full DDL chars | Est. LH tokens | Est. DDL tokens | Naive DDL chars | Example subset ratio |
|---|---:|---:|---:|---:|---:|---:|---:|
| Startup SaaS | 30 | 1131 | 11940 | 282 | 2985 | 17316 | 3 tables = 10.9% of full DDL (1304 / 11940 chars) |
| Enterprise ERP | 200 | 10515 | 155988 | 2628 | 38997 | 189532 | 8 tables = 4.0% of full DDL (6261 / 155988 chars) |
| Legacy Nightmare | 500 | 17324 | 372252 | 4331 | 93063 | 439324 | 15 tables = 2.3% of full DDL (8644 / 372252 chars) |

Local compiler microbenchmarks on the same machine:

| Operation | Time/op |
|---|---:|
| `CompileAll` on 30 tables | 0.083 ms |
| `CompileAll` on 200 tables | 1.50 ms |
| `CompileAll` on 500 tables | 4.10 ms |
| `CompileSubset` 15 of 500 | 0.130 ms |
| `CompileLighthouse` on 500 tables | 0.564 ms |

Measured DDL-vs-naive savings in the same tests are modest:

- Startup SaaS: 31.0%
- Enterprise ERP: 17.7%
- Legacy Nightmare: 15.3%

The main benefit is selection, not compression. The tests also include adversarial cases:

- 60-character table names push lighthouse density to 18.4 estimated tokens per table.
- A single 80-column table is still 82.1% of a naive pg_dump-style rendering.

Benchmark timings above were recorded with `go test -bench` on an 11th Gen Intel i7-11370H using the synthetic fixtures in `internal/compile/bench_honest_test.go`.

## Agentic benchmark

Preliminary result (single run, n=1).

We ran an end-to-end agentic benchmark comparing two arms on the same seeded Postgres database (20K+ rows) with the same 7 questions, same system prompt, and the same model (Claude Sonnet). The injected benchmark context covered 8 public-schema tables; `auth.sessions` existed separately in the database:

- **Baseline**: Postgres MCP tool only. The model must discover the schema on its own (the baseline agent typically queries `information_schema` or guesses).
- **dbdense**: Same Postgres MCP tool, plus precompiled schema DDL injected into the prompt context via `-context-file-dbdense`.

| Metric | Baseline | dbdense | Delta |
|--------|----------|---------|-------|
| Correct answers | 4/7 | 6/7 | +50% more correct |
| Avg turns | 5.1 | 3.0 | -41% fewer round-trips |
| Total prompt tokens | 648,984 | 513,927 | -21% fewer tokens |
| Avg latency | 39,960 ms | 33,273 ms | -17% faster |

Key findings:

- Schema context improves accuracy, not just token count. Without schema context, the baseline guessed at column semantics and counted wrong statuses (q1: 14,000 vs correct 9,000) and miscalculated revenue (q5). With schema context — column types, FK relationships, table descriptions — dbdense produced correct queries for both.
- q3, the stress scenario requiring GROUP BY across providers and regions, was answered correctly in both arms in the recorded run.
- The verification harness uses strict JSON key matching, which still undercounts accuracy on several other questions when the model returns the right number under a different key name.

Caveats: this is a single run (n=1) on a small schema. It is not a large-scale study. These results are from one benchmark run. Run the harness yourself to reproduce:

```bash
cd benchmark && go run ./cmd/benchrun -model claude-sonnet-4-20250514 -arms baseline,dbdense -mcp-config-baseline mcp_postgres.json -mcp-config-dbdense mcp_postgres.json -context-file-dbdense dbdense.schema.sql -claude-permission-mode bypassPermissions -runs 1
```

## Supported backends

- PostgreSQL
- MongoDB

New backends can be added by implementing the `Extractor` interface in `internal/extract`.

## Install

```bash
go install github.com/valkdb/dbdense/cmd/dbdense@latest
```

## Quick start

```bash
dbdense export --driver postgres --db "postgres://user:pass@localhost:5432/app"
dbdense compile --mode lighthouse --in ctxexport.json --out lighthouse.txt
dbdense compile --in ctxexport.json --out schema.sql
dbdense serve --in ctxexport.json
```

If you use Claude Code, `dbdense init-claude` writes the `.mcp.json` entry for you:

<img src="docs/media/demo-claude.svg" alt="Claude Code integration" width="820">

After setup, Claude can review code, write queries, and plan migrations with full schema awareness — without database credentials or network access.

## Sidecar enrichment

`dbdense.yaml` lets you layer human-written descriptions onto the extracted snapshot without changing the source database. Those descriptions are merged during `export` and then flow into the rendered DDL as SQL comments.

## Honest limitations

- The snapshot is static. Re-run `export` after schema changes.
- The runtime path is intentionally offline. If you need live schema introspection at query time, use a live database MCP server instead.
- `slice` selection still depends on the LLM asking for the right tables. dbdense reduces context size; it does not solve table selection for the model.
- The DDL renderer includes columns, PKs, FKs, NOT NULL, DEFAULTs, unique constraints, and indexes, but it is not a full `pg_dump --schema-only` replacement.
- MongoDB extraction is sample-based. Field inference depends on sampled documents, and inferred refs are conservative: only exact `*_id -> collection_name` matches become edges.
- The performance numbers above come from synthetic fixtures and local benchmarks, not a published real-world production corpus.

## Use cases

- **Offline schema context for LLM agents** — the primary use case. Give AI coding assistants full schema awareness without a live database connection.

The `ctxexport.json` artifact is a machine-readable schema snapshot that other tools can consume:

- **Database linting** — external linters can check naming conventions, missing indexes, or FK integrity against the snapshot without a live connection.
- **Schema diffing** — diff snapshots across environments or over time to detect drift.
- **CI validation** — check schema structure in pipelines without provisioning a database.

dbdense does not include linting, diffing, or CI tools — it produces the artifact they can read.

## Docs

- `docs/getting-started.md`
- `docs/architecture.md`
- `docs/slice-mechanics.md`
- `docs/claude-code-integration.md`
- `docs/ctxexport-contract.md`
- `docs/demo-guide.md`
- `docs/benchmark-fairness.md`
- `docs/roadmap.md`

## Development

```bash
go test ./...
```

Integration tests need the seeded Docker databases:

```bash
docker compose -f docker-compose.test.yml up -d
go test -tags integration ./...
```
