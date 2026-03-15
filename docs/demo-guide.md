# Demo Guide

This guide is for a quick end-to-end run that matches the current CLI and MCP output.

## Synthetic demo

Build and generate a local export:

```bash
go build -o dbdense ./cmd/dbdense
./dbdense genexport --tables 25 --out demo_export.json
```

Current stderr format:

```text
generated <entities> entities, <edges> edges -> demo_export.json
```

Compile lighthouse and full DDL:

```bash
./dbdense compile --mode lighthouse --in demo_export.json --out demo_lighthouse.txt
./dbdense compile --in demo_export.json --out demo_schema.sql
```

Current stderr formats:

```text
lighthouse: <tables> tables, ~<tokens> tokens -> demo_lighthouse.txt
compiled <entities> entities, <edges> edges -> demo_schema.sql
```

Check the lighthouse file:

```text
# lighthouse.v0
# Table map. T=table, J=joined tables. Use slice tool for column details.
T:users|J:orders
T:orders|J:payments,users
```

Check the DDL file:

```sql
-- dbdense schema context
CREATE TABLE users (
  id uuid PRIMARY KEY,
  email text
);
```

## Live export demo

PostgreSQL:

```bash
dbdense export --driver postgres --db "postgres://user:pass@localhost:5432/app"
```

MongoDB:

```bash
dbdense export --driver mongodb --db "mongodb://localhost:27017" --schemas "appdb"
```

Current stderr format:

```text
exported <entities> entities, <edges> edges -> ctxexport.json
```

Warnings, when present, are printed as:

```text
WARNING: <message>
```

## MCP demo

Start the server:

```bash
dbdense serve --in ctxexport.json
```

Current stderr output:

```text
starting MCP stdio server
```

What the client gets:

- resource `dbdense://lighthouse`
- tool `slice`
- tool `reset`

Example `slice` behavior:

```text
-- dbdense schema context
CREATE TABLE users (...)
CREATE TABLE orders (...)

-- Warning: tables not found in schema: fake_table
-- Note: skipped (already in context): users
```

If every requested table was already sent in the same session, the tool returns:

```text
All requested tables are already in your context: users, orders
```

The `reset` tool returns:

```text
Session cache cleared. All tables will be re-sent on next slice call.
```

## Claude Code setup demo

Generate `.mcp.json`:

```bash
dbdense init-claude --in ctxexport.json
```

Typical stderr:

```text
adding dbdense to .mcp.json
done. Restart Claude to pick up the new MCP server.
```

If the entry already exists:

```text
updating existing dbdense entry in .mcp.json
done. Restart Claude to pick up the new MCP server.
```

Generated config shape:

```json
{
  "mcpServers": {
    "dbdense": {
      "command": "/absolute/path/to/dbdense",
      "args": ["serve", "--in", "/path/to/ctxexport.json"]
    }
  }
}
```

## Suggested smoke checks

- `jq '.version' ctxexport.json` should be `"ctxexport.v0"`
- `head -5 demo_lighthouse.txt` should start with `# lighthouse.v0`
- `head -5 demo_schema.sql` should start with `-- dbdense schema context`
- Claude should be able to list tables from the lighthouse and then call `slice` for detail
