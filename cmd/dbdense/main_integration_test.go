//go:build integration

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/valkdb/dbdense/internal/extract"
	"github.com/valkdb/dbdense/pkg/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func pgDSNIntegration(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DBDENSE_PG_DSN")
	if dsn == "" {
		dsn = "postgres://dbdense:dbdense@localhost:5432/dbdense_test?sslmode=disable"
	}
	return dsn
}

func mongoURIIntegration(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("DBDENSE_MONGO_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	return uri
}

func requirePostgresSeeded(t *testing.T) {
	t.Helper()
	db, err := sql.Open("pgx", pgDSNIntegration(t))
	if err != nil {
		t.Fatalf("postgres connect failed: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("postgres ping failed: %v\nHint: start seeded DB: docker compose -f docker-compose.test.yml up -d", err)
	}

	for _, table := range []string{"public.users", "auth.sessions"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("postgres table check failed for %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("required seeded table missing: %s", table)
		}
	}
}

func requireMongoSeeded(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURIIntegration(t)))
	if err != nil {
		t.Fatalf("mongo connect failed: %v", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("mongo ping failed: %v\nHint: start seeded DB: docker compose -f docker-compose.test.yml up -d", err)
	}

	db := client.Database("dbdense_test")
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		t.Fatalf("mongo list collections failed: %v", err)
	}
	sort.Strings(names)
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	for _, coll := range []string{"users", "orders"} {
		if !set[coll] {
			t.Fatalf("required seeded collection missing: %s", coll)
		}
	}
}

func writeExport(t *testing.T, path string, export *schema.CtxExport) {
	t.Helper()
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write export file: %v", err)
	}
}

func TestCompileSplitBySchema_PostgresIntegration(t *testing.T) {
	requirePostgresSeeded(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ext := &extract.PostgresExtractor{
		DSN:     pgDSNIntegration(t),
		Schemas: []string{"public", "auth"},
	}
	export, err := ext.Extract(ctx)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "ctxexport.json")
	outDir := filepath.Join(tmpDir, "ctxpacks")
	writeExport(t, inPath, export)

	cmd := newCompileCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"--in", inPath,
		"--split-by", "schema",
		"--out-dir", outDir,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("compile split command failed: %v", err)
	}

	defaultPack, err := os.ReadFile(filepath.Join(outDir, "default.ctxpack.txt"))
	if err != nil {
		t.Fatalf("read default pack: %v", err)
	}
	authPack, err := os.ReadFile(filepath.Join(outDir, "auth.ctxpack.txt"))
	if err != nil {
		t.Fatalf("read auth pack: %v", err)
	}

	if !strings.Contains(string(defaultPack), "CREATE TABLE users") {
		t.Fatalf("default pack missing users entity")
	}
	if !strings.Contains(string(authPack), `CREATE TABLE auth.sessions`) {
		t.Fatalf("auth pack missing auth.sessions entity")
	}
}

func TestCompileSplitBySchema_MongoIntegration(t *testing.T) {
	requireMongoSeeded(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ext := &extract.MongoExtractor{
		URI:      mongoURIIntegration(t),
		Database: "dbdense_test",
	}
	export, err := ext.Extract(ctx)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "ctxexport.json")
	outDir := filepath.Join(tmpDir, "ctxpacks")
	writeExport(t, inPath, export)

	cmd := newCompileCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"--in", inPath,
		"--split-by", "schema",
		"--out-dir", outDir,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("compile split command failed: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read outDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "default.ctxpack.txt" {
		t.Fatalf("mongo split expected only default.ctxpack.txt, got %v", entries)
	}
}
