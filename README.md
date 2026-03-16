# dbdense - Schema context that fits in a prompt

Most LLM questions only need a few tables, but tools send the whole schema. dbdense is an offline schema extraction and compile pipeline: it extracts your database schema once, builds a compact schema map, and renders detailed schema text on demand. It can also serve the result over MCP.

<img src="docs/media/demo.svg" alt="dbdense demo" width="820">

## The problem

A 500-table schema is ~93K tokens of DDL. Most questions touch 3-5 tables.

**Without schema context**, the agent guesses at column names, misinterprets statuses, and burns tokens querying `information_schema` to orient itself.

**With dbdense**, a ~4K token schema map stays in context. The agent sees every exported entity and FK relationship at a glance, then requests detailed schema text only for the objects it needs.

In the current checked-in n=3 benchmark run (same model, same 5 questions, same seeded database):

| Metric | Without schema context | With dbdense | Delta |
|--------|------------------------|--------------|-------|
| Correct answers | 13/15 | 13/15 | equal |
| Avg turns | 4.1 | 2.2 | -46% fewer round-trips |
| Avg total tokens per question | 57,184 | 37,521 | -34% fewer tokens |
| Avg latency | 34s | 31s | -8% faster |

Both arms achieved the same accuracy, but dbdense used 34% fewer total tokens per question and 46% fewer turns. The savings scale with query complexity: on multi-table joins, baseline spent up to 6 turns discovering schema while dbdense answered in 2. This run used precompiled schema DDL injected into the prompt, not the live MCP slice flow, and it still misses the report's latency and stress gates. See [Agentic benchmark](#agentic-benchmark) for methodology and caveats.

## Install

Requires Go 1.25+.

```bash
go install github.com/valkdb/dbdense/cmd/dbdense@latest
```

This installs the `dbdense` binary. The `benchmark/` directory is a separate Go module used for end-to-end testing and is not included in the install.

## Quick start

```bash
dbdense export --driver postgres --db "postgres://user:pass@localhost:5432/app" --schemas public
dbdense compile --mode lighthouse --in ctxexport.json --out lighthouse.txt
dbdense compile --in ctxexport.json --out schema.sql
dbdense serve --in ctxexport.json
```

With the offline schema compiled, an LLM can plan complex multi-table joins using only the local artifact — no database credentials or network access:

<img src="docs/media/demo-claude.svg" alt="Offline schema-aware query planning" width="820">

If you use Claude Code, `dbdense init-claude` writes the `.mcp.json` entry for you. See `docs/claude-code-integration.md` for details.

## Two-tier context model

- **lighthouse** — a compact schema map kept in context for broad schema awareness.
  - Exposed as the `dbdense://lighthouse` MCP resource.
  - Contains exported entity names plus FK neighbors.
  - ~4K tokens for 500 tables.
- **slice** — compiled schema text returned on demand for only the entities the agent asks about.
  - Returned through the MCP `slice` tool.
  - SQL DDL for tables and materialized views, plus `-- VIEW:` comments for views.
  - Session dedup avoids re-sending the same tables on later turns.

Example lighthouse:

```text
# lighthouse.v0
# Table map. T=table, J=joined tables, E=embedded docs. Use slice tool for column details.
T:users|J:orders,sessions
T:orders|E:payload,shipping|J:payments,shipments,users
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

## What you get

- **Fewer turns** — schema context eliminated discovery round-trips. In our benchmark, average turns dropped from 4.1 to 2.2 per question.
- **Fewer tokens** — a 500-table schema compresses from ~93K tokens to a ~4K lighthouse map, with detailed schema text served only on demand.
- **No credentials in the agent runtime** — export once, then compile and serve from the local snapshot. Works offline and air-gapped.
- **Repo-committable artifacts** — the compiled lighthouse and schema files are plain text. Check them into version control alongside your code.

## Architecture

`Extract -> Compile -> Serve`

1. `export` reads PostgreSQL or MongoDB metadata once and writes `ctxexport.json`.
2. `compile` renders either a lightweight lighthouse map or SQL-first schema text from that snapshot.
3. `serve` optionally exposes the snapshot over MCP:
   - Resource: `dbdense://lighthouse`
   - Tool: `slice`
   - Tool: `reset`

After `export`, the runtime path is local-only. `compile`, `serve`, and Claude/MCP usage do not need database credentials or a live database connection. That makes the tool usable in offline and air-gapped environments, and it keeps production credentials out of the agent runtime.

## Supported backends

- **PostgreSQL** — extracts tables, views, materialized views, columns (types, NOT NULL, defaults), primary keys, foreign keys, unique constraints, and indexes from `pg_catalog`. Does not extract triggers, RLS policies, custom types, or functions.
- **MongoDB** — extracts JSON Schema validators when present, otherwise samples documents to infer fields and types. Extracts indexes (compound, unique). Inferred refs are conservative: only exact `*_id → collection_name` matches.

New backends can be added by implementing the `Extractor` interface in `internal/extract`.

## Sidecar enrichment

`dbdense.yaml` lets you layer human-written descriptions and column value annotations onto the extracted snapshot without changing the source database. These are merged during `export` and flow into the rendered DDL as SQL comments.

Example sidecar:

```yaml
entities:
  payments:
    fields:
      status:
        values: ["pending", "authorized", "paid", "failed", "refunded"]
  orders:
    fields:
      status:
        description: "Order lifecycle status."
        values: ["pending", "confirmed", "shipped", "delivered", "cancelled"]
```

Column values appear in the compiled DDL as inline comments (e.g., `-- Values: pending, authorized, paid, failed, refunded`), giving the LLM knowledge of valid filter values without querying the database.

## Performance

Numbers below come from the compile test suite and Go benchmarks in `internal/compile` and measure **compile-time artifact sizes**, not end-to-end model token usage. Token counts are heuristic estimates based on `chars / 4`, which the tests note is roughly 15-25% optimistic for DDL. For actual model token usage from the agentic benchmark, see [Agentic benchmark](#agentic-benchmark).

| Fixture | Tables | Lighthouse chars | Full DDL chars | Est. LH tokens | Est. DDL tokens | Naive DDL chars | Example subset ratio |
|---|---:|---:|---:|---:|---:|---:|---:|
| Startup SaaS | 30 | 1148 | 11940 | 287 | 2985 | 17316 | 3 tables = 10.9% of full DDL (1304 / 11940 chars) |
| Enterprise ERP | 200 | 10532 | 155988 | 2633 | 38997 | 189532 | 8 tables = 4.0% of full DDL (6261 / 155988 chars) |
| Legacy Nightmare | 500 | 17341 | 372252 | 4335 | 93063 | 439324 | 15 tables = 2.3% of full DDL (8644 / 372252 chars) |

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

The current checked-in benchmark run is an end-to-end agentic benchmark (n=3) comparing two arms on the same seeded Postgres database (20K+ rows) with 5 questions, the same system prompt, and the same model (Claude Sonnet 4). The schema context covered 8 public-schema tables with sidecar-provided column value annotations:

- **Baseline**: Postgres MCP tool only. The model must discover the schema on its own (typically queries `information_schema`).
- **dbdense**: Same Postgres MCP tool, plus precompiled schema DDL injected into the prompt context via `-context-file-dbdense`.

| Metric | Baseline | dbdense | Delta |
|--------|----------|---------|-------|
| Correct answers | 13/15 | 13/15 | equal |
| Avg turns | 4.1 | 2.2 | -46% fewer round-trips |
| Avg total tokens per question | 57,184 | 37,521 | -34% fewer tokens |
| Avg latency | 34s | 31s | -8% faster |

Per-scenario breakdown (averaged over 3 runs):

| Scenario | Baseline tokens | dbdense tokens | Baseline turns | dbdense turns |
|----------|----------------:|---------------:|---------------:|--------------:|
| q1 (simple filter) | 72,394 | 36,864 | 6.3 | 3.0 |
| q2 (complex multi-join) | 88,098 | 32,130 | 6.3 | 2.0 |
| q3 (revenue sum) | 30,686 | 35,594 | 2.0 | 2.0 |
| q4 (shipment status) | 31,560 | 39,129 | 2.0 | 2.0 |
| q5 (refund + cancel) | 63,183 | 43,885 | 3.7 | 2.0 |

Key findings:

- Token and turn savings scale with query complexity. On multi-table joins (q1, q2, q5), dbdense used 46-64% fewer tokens because baseline spent extra turns querying `information_schema` to discover the schema. On simple queries (q3, q4), both arms performed similarly.
- Both arms achieved 13/15 accuracy. Baseline missed q2 once and q4 once; dbdense missed q2 twice. One repeated failure mode was q2 returning 747 instead of 49.
- The benchmark used precompiled schema DDL injected into the prompt, not the live MCP slice flow.

Caveats: this is a small-scale benchmark on a synthetic schema. Results are specific to this setup, and this run still misses the latency and stress gates in the benchmark report. Run the harness yourself to reproduce:

```bash
cd benchmark && go run ./cmd/benchrun -model claude-sonnet-4-20250514 -arms baseline,dbdense -mcp-config-baseline mcp_postgres.json -mcp-config-dbdense mcp_postgres.json -context-file-dbdense dbdense.schema.sql -claude-permission-mode bypassPermissions -runs 3
```

## Honest limitations

- The snapshot is static. Re-run `export` after schema changes.
- The runtime path is intentionally offline. If you need live schema introspection at query time, use a live database MCP server instead.
- `slice` selection still depends on the LLM asking for the right tables or views. dbdense reduces context size; it does not solve object selection for the model.
- The DDL renderer includes columns, PKs, FKs, NOT NULL, DEFAULTs, unique constraints, and indexes, but it is not a full `pg_dump --schema-only` replacement.
- MongoDB extraction is sample-based by default. When a collection has a JSON Schema validator (`$jsonSchema`), it is used as ground truth. Otherwise, field inference depends on sampled documents. Inferred refs are conservative: only exact `*_id -> collection_name` matches become edges. When a `*_id` field is >=90% objectId but has no matching collection, a high-confidence warning is emitted so you can resolve it via sidecar.
- The performance numbers above come from synthetic fixtures and local benchmarks, not a published real-world production corpus.

## Use cases

- **Schema context for LLM agents** — the primary use case. Give AI coding assistants full schema awareness without a live database connection.

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
