#!/bin/bash
# Demo script for dbdense — run against the test Postgres DB
set -e
export PATH="/tmp:$PATH"

DEMO_DIR=$(mktemp -d)
cd "$DEMO_DIR"

echo "$ dbdense export --driver postgres --db 'postgres://dbdense:dbdense@localhost:5432/dbdense_test' --schemas public"
dbdense export --driver postgres --db "postgres://dbdense:dbdense@localhost:5432/dbdense_test" --schemas public

echo ""
echo "$ dbdense compile --mode lighthouse --in ctxexport.json --out lighthouse.txt"
dbdense compile --mode lighthouse --in ctxexport.json --out lighthouse.txt

echo ""
echo "$ cat lighthouse.txt"
cat lighthouse.txt

echo ""
echo "$ dbdense compile --in ctxexport.json --out schema.sql"
dbdense compile --in ctxexport.json --out schema.sql

echo ""
echo "$ head -40 schema.sql"
head -40 schema.sql

echo ""
echo "# Ready to serve via MCP: dbdense serve --in ctxexport.json"

rm -rf "$DEMO_DIR"
