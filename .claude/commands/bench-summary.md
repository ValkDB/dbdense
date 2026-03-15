Summarize the benchmark run at stamp `$ARGUMENTS`.

## Steps

1. Read the raw records from `benchmark/results/$ARGUMENTS/runs.jsonl`. Parse each JSON line.

2. Read all report markdown files in `benchmark/results/$ARGUMENTS/` (report_dbdense.md).

3. Read `benchmark/results/$ARGUMENTS/manifest.json` for run metadata.

4. Produce an **honest** summary in this exact format:

---

### Benchmark Run `$ARGUMENTS`

**Meta**: [model_id] | [total records] records | [date]

#### Accuracy (the primary metric)

| Arm | q1_simple | q2_complex | q3_stress | Regular Pass Rate |
|-----|-----------|------------|-----------|-------------------|

For each cell: PASS/FAIL. This is the most important table.

#### Token Breakdown (honest numbers)

| Arm | Est. Input Tokens (median) | Completion Tokens (median) | Input Overhead vs Baseline |
|-----|---------------------------|---------------------------|---------------------------|

Call out explicitly:
- In agentic MCP mode, baseline uses extra tokens for schema discovery tool calls
- dbdense skips discovery by having schema context upfront
- The delta reflects total conversation cost including all tool-call round-trips

#### Completion Token Efficiency

For each arm vs baseline, report:
- Completion token change (% reduction or increase)
- Whether the context caused more verbose output (it shouldn't)

#### Latency

| Arm | Median Wall Latency (ms) | vs Baseline |
|-----|-------------------------|-------------|

#### Stress Test Results

Report q3_stress per arm. Highlight which arms pass and which fail. This demonstrates the **accuracy value** of providing schema context.

#### Gate Status

List all 7 gates with PASS/FAIL and the key metric value. Note which gates fail due to n=1 runs (expected for smoke runs).

#### Honest Bottom Line

Write 3-5 sentences summarizing:
1. Whether dbdense improves **accuracy** (the primary claim)
2. The **token savings** from skipping schema discovery
3. Whether **completion tokens** are comparable/better/worse
4. Whether **latency** improved or regressed
5. What would change with more runs (n=1 limitations)

Be specific with numbers, not vague.

---

## Rules

- Always separate input vs output token metrics
- The value proposition is **accuracy + fewer tool calls + lower total tokens**
- If completion tokens are higher for dbdense, say so and explain it's output variance
- Report raw numbers, not just percentages
- Flag any incomplete/failed runs explicitly
