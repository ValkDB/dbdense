# Slice Mechanics

dbdense uses two tiers of schema context.

## Tier 1: lighthouse

`dbdense://lighthouse` is the lightweight schema map exposed as an MCP resource. Some clients may also keep it in prompt context.

It contains:

- table names
- FK neighbors
- no columns
- no types

Why it exists:

- it makes the full table map available at low cost
- it gives the model enough topology to choose what to inspect next

Measured size from the compile tests:

- 30-table fixture: 282 estimated tokens
- 200-table fixture: 2628 estimated tokens
- 500-table fixture: 4331 estimated tokens

The same tests show the usual caveat: long names degrade density. In the adversarial long-name case, lighthouse rises to 18.4 estimated tokens per table.

## Tier 2: slice

The MCP `slice` tool returns DDL for only the requested tables:

```json
{"tables":["users","orders"]}
```

Response shape:

- starts with `-- dbdense schema context`
- contains `CREATE TABLE` statements for the matched tables
- includes FK statements only when both endpoints are in the requested subset

This is where most of the context savings come from. In the benchmark fixtures:

- 3 tables out of 30 produced 10.9% of the full DDL (1304 / 11940 chars)
- 8 tables out of 200 produced 4.0% of the full DDL (6261 / 155988 chars)
- 15 tables out of 500 produced 2.3% of the full DDL (8644 / 372252 chars)

## Session dedup

The server keeps an in-memory set of table names already sent in the current session.

Effects:

- if the model asks for `users` twice, the second response does not resend the same DDL
- if some requested tables are new and some were already sent, only the new tables are rendered
- if everything was already sent, the tool returns a short message instead of another DDL block

This reduces repeated prompt growth in multi-turn conversations.

## Warning and note behavior

Current tool behavior:

- nonexistent tables append:
  - `-- Warning: tables not found in schema: ...`
- already-sent tables append:
  - `-- Note: skipped (already in context): ...`

This is intentionally plain text so the model can keep using the DDL block directly.

## Reset tool

`reset` clears the session dedup cache.

Use it when:

- you want previously delivered tables to be sent again
- the schema file changed and the client kept the same MCP session open

It does not reload the export or re-extract the schema. It only resets the "already sent" bookkeeping.

## Accuracy benefit of pre-loaded context

In the preliminary agentic benchmark, having precompiled schema context injected up front reduced errors that stem from schema discovery. In that run the `dbdense` arm received compiled schema DDL via `-context-file-dbdense`, not live `slice` calls. Without context, the baseline model guessed at column semantics — for example, counting wrong order statuses and miscalculating revenue. With schema context available, the model produced correct queries for the same questions. See the [README agentic benchmark section](../README.md#agentic-benchmark) for the full results.

## Limitations

- The model still has to choose the right tables. dbdense reduces context size; it does not solve table selection.
- lighthouse omits columns and types by design.
- dedup is per session, not persistent.
- the runtime is static. Schema changes require a new `export`.
