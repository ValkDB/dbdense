# Roadmap

## Current state

Shipped today:

- PostgreSQL extraction
- MongoDB extraction with sampled field inference
- `ctxexport.json` as the offline schema snapshot
- `dbdense.yaml` sidecar merge
- DDL compiler
- lighthouse compiler
- MCP stdio server with `dbdense://lighthouse`, `slice`, and `reset`
- session dedup for repeated slice calls
- `init-claude`
- synthetic export generation
- split-by-schema compile output
- benchmark harness and report generator

## Near-term work

- run and publish more end-to-end benchmark results
- collect results beyond the current synthetic compile fixtures
- improve docs and examples around sidecar usage and MongoDB behavior
- add more extraction fidelity where it clearly helps the agent

## Possible later work

- more backends if there is real demand
- additional benchmark scenarios for larger and messier schemas
- better operational guidance for keeping exports fresh in CI

## Deliberately not in scope right now

- live database access from the dbdense MCP server at runtime
- pluralization or name-guessing for MongoDB inferred refs
- replacing standard SQL DDL with a custom dense schema language

If you need live schema discovery or live data access during agent execution, use a database MCP server alongside dbdense rather than expecting dbdense to become one.
