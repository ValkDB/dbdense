# Benchmark Fairness

> **Note:** Benchmark results from the agentic harness are preliminary and not yet publishable. The compile-suite metrics in the README are from deterministic Go tests.

This repo includes a benchmark harness, but any performance claims should be treated as preliminary until the results are rerun and published with raw artifacts.

The goal of the harness is simple: compare agents that have to discover schema context on the fly against agents that receive precompiled dbdense context, while keeping the rest of the setup constant.

## Arms

The harness currently supports:

- `baseline`
- `dbdense`
- `dbdense_lighthouse`
- `dbdense_with_desc` (optional)

Default `benchrun` arms are:

- `baseline`
- `dbdense`
- `dbdense_lighthouse`

## Fairness rules

For a fair comparison, keep these fixed across arms:

- same provider
- same model
- same system prompt
- same scenario prompts
- same seeded database snapshot
- same MCP configuration presence

The runner validates the MCP part. If `baseline` is given an MCP config, the dbdense arms must also be given one. If `baseline` is run without MCP, the others must match that mode.

## What is measured

Per run, the harness records:

- provider-reported `prompt_tokens`
- provider-reported `completion_tokens`
- provider-reported `total_tokens`
- `wall_latency_ms`
- `num_turns`
- tool-call count details when the provider reports them
- `accuracy.score`
- `accuracy.pass`
- `estimated_input_tokens` as a legacy local estimate

Reports should display `total_reported_token` (the provider-reported sum) rather than `estimated_input_tokens` (the legacy local chars/4 estimate) for token comparisons.

Interpretation:

- prompt tokens measure the full context cost the provider processed
- latency measures end-to-end elapsed time
- turns matter because agentic discovery usually resends more history
- accuracy is the final check on whether the answer was actually correct

## What is not measured

The harness does not currently measure:

- the one-time `export` cost
- database CPU or planner behavior
- human usefulness of the returned SQL
- prompt-engineering effort outside the shared benchmark prompt
- live-schema drift over time
- non-PostgreSQL benchmark backends

That is intentional. The benchmark is about end-to-end agent behavior during question answering, not about every operational cost around it.

## Methodology

Scenarios use natural-language business questions and expected JSON answers.

High-level flow:

1. open a provider session per arm
2. run the regular scenarios in that session
3. run stress scenarios separately
4. write `runs.jsonl`
5. aggregate with `benchreport`

`benchreport` then computes summaries and gate results around completeness, run counts, accuracy, token overhead, latency, session continuity, and stress behavior.

## Preliminary disclaimer

The harness is useful today, but the published evidence is still narrow:

- scenario sets are small
- seeded datasets are synthetic
- provider behavior changes over time
- results from one model or one provider should not be generalized automatically

Use the numbers as evidence about this setup, not as universal LLM behavior.

## Reproducing

Prepare context files from the repo root:

```bash
go run ./cmd/dbdense compile --in ctxexport.json --out benchmark/dbdense.schema.sql
go run ./cmd/dbdense compile --mode lighthouse --in ctxexport.json --out benchmark/dbdense.lighthouse.txt
```

Run the benchmark:

```bash
cd benchmark
go run ./cmd/benchrun \
  --provider claude \
  --model <model-id> \
  --runs 3 \
  --arms baseline,dbdense,dbdense_lighthouse \
  --claude-permission-mode bypassPermissions \
  --mcp-config-baseline "./mcp_postgres.json" \
  --mcp-config-dbdense "./mcp_postgres.json" \
  --mcp-config-dbdense-lighthouse "./mcp_postgres.json" \
  --context-file-dbdense "./dbdense.schema.sql" \
  --context-file-dbdense-lighthouse "./dbdense.lighthouse.txt"
```

Generate a report:

```bash
go run ./cmd/benchreport --input ./results/<run_stamp> --target-arm dbdense
```

Artifacts live under `benchmark/results/<run_stamp>/`.

## Reporting guidance

When publishing a result, include:

- the raw artifact directory
- the exact model and provider
- the exact arm list
- whether MCP was enabled
- the median token and latency summaries
- the pass/fail summaries
- the caveat that results are preliminary unless replicated across multiple runs and environments
