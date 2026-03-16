# Claude Code Integration

<img src="media/demo-claude.svg" alt="Claude Code integration demo" width="820">

dbdense runs as a local MCP stdio server. Claude Code talks to it over stdin/stdout, and dbdense reads only local files at runtime.

That means:

- no database credentials at runtime
- no network listener
- no live database connection once `ctxexport.json` exists

## What Claude sees

dbdense currently exposes one resource and two tools:

- resource `dbdense://lighthouse`
- tool `slice`
  - parameter: `tables` as an array of strings
- tool `reset`
  - no parameters

A common conversation shape, when the client reads the resource, is:

1. Claude reads or is given `dbdense://lighthouse`
2. Claude decides which objects matter
3. Claude calls `slice` for those objects
4. On later turns, Claude only gets new objects unless it calls `reset`

## Quick setup

Export a snapshot first:

```bash
dbdense export --driver postgres --db "postgres://user:pass@localhost:5432/app" --schemas public
```

Then generate the Claude config:

```bash
dbdense init-claude --in ctxexport.json
```

For Claude Desktop:

```bash
dbdense init-claude --target claude-desktop --in ctxexport.json
```

## Manual `.mcp.json`

```json
{
  "mcpServers": {
    "dbdense": {
      "command": "dbdense",
      "args": ["serve", "--in", "ctxexport.json"]
    }
  }
}
```

`init-claude` usually writes an absolute path to the binary. That is fine and avoids PATH issues.

## What the tools return

`dbdense://lighthouse` returns:

```text
# lighthouse.v0
# Table map. T=table, J=joined tables, E=embedded docs. Use slice tool for column details.
T:users|J:orders,sessions
T:orders|J:payments,users
```

`slice` returns compiled schema text for the requested names. Tables and materialized views are rendered as `CREATE TABLE` DDL; views are rendered as `-- VIEW:` comments. If a table name is wrong, the response appends:

```text
-- Warning: tables not found in schema: ...
```

If some tables were already sent earlier in the session, the response appends:

```text
-- Note: skipped (already in context): ...
```

If everything was already sent, `slice` returns a short message instead of another DDL block.

`reset` returns:

```text
Session cache cleared. All tables will be re-sent on next slice call.
```

## Sidecar guidance

If the raw schema is missing useful descriptions, add them to `dbdense.yaml` and re-run `export`. The sidecar is merged at export time and then becomes part of the offline snapshot.

## Troubleshooting

Claude does not see the server:

- check that `.mcp.json` is in the project root where Claude Code is running
- check that `ctxexport.json` exists at the configured path
- check that the `command` in `.mcp.json` points to a valid `dbdense` binary

Schema looks stale:

- re-run `dbdense export`
- if the Claude session is already open, call `reset` after restarting the server

Too much schema in the prompt:

- if the client auto-includes resource text, keep only the lighthouse there
- let Claude fetch detail with `slice`
- reduce PostgreSQL export scope with `--schemas` when needed
