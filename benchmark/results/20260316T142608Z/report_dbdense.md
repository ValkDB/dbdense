# Benchmark Report

- Generated at: `2026-03-16T14:52:34Z`
- Input: `results/20260316T142608Z`
- Records: `30` (`30` complete)
- Baseline arm: `baseline`
- Target arm: `dbdense`

## Regular Summary

| Arm | Runs | Complete | Pass Rate | Median Completion Tokens | Median Est. Input Tokens | Median Latency (ms) |
|---|---:|---:|---:|---:|---:|---:|
| baseline | 15 | 15 | 0.867 | 922 | 36 | 29689 |
| dbdense | 15 | 15 | 0.867 | 789 | 1130 | 27116 |

## Stress Summary

| Arm | Runs | Complete | Pass Rate | Avg Score |
|---|---:|---:|---:|---:|

## Baseline vs Target

- Completion token reduction (median): `14.43%`
- Baseline est. input tokens (median): `36`
- Target est. input tokens (median): `1130`
- Latency reduction (median): `8.67%`
- Pass-rate delta: `0.0000`
- Completion token p-value (Mann-Whitney U): `0.740022`
- Latency p-value (Mann-Whitney U): `0.771551`

## Acceptance Gates

- [PASS] G1 No missing token/time/accuracy fields: missing=0
- [PASS] G2 >= 3 complete runs per arm per scenario: underfilled_cells=0
- [PASS] G3 Target accuracy is not lower than baseline: target_pass_rate=0.8667 baseline_pass_rate=0.8667
- [PASS] G4 Completion token overhead <= 25% vs baseline: completion_overhead_pct=-14.43 baseline_median=922 target_median=789
- [FAIL] G5 Latency reduction >= 15% (median): latency_reduction_pct=8.67
- [PASS] G6 Session continuity preserved per arm iteration: bad_groups=0
- [FAIL] G7 Stress gate: target >= 90% pass rate and baseline fails: target_pass_rate=0.0000 baseline_pass_rate=0.0000
