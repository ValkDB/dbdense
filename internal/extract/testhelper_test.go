//go:build integration

package extract_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/valkdb/dbdense/internal/extract"
	"github.com/valkdb/dbdense/pkg/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	pgSeedOnce    sync.Once
	mongoSeedOnce sync.Once

	pgExportOnce      sync.Once
	pgExportCache     *schema.CtxExport
	pgExportErr       error
	pgMultiExportOnce sync.Once
	pgMultiExport     *schema.CtxExport
	pgMultiExportErr  error
	mongoExportOnce   sync.Once
	mongoExportCache  *schema.CtxExport
	mongoExportErr    error
)

func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DBDENSE_PG_DSN")
	if dsn == "" {
		dsn = "postgres://dbdense:dbdense@localhost:5432/dbdense_test?sslmode=disable"
	}
	return dsn
}

func mongoURI(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("DBDENSE_MONGO_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	return uri
}

// seedPostgres validates that integration DB is already initialized via
// testdata/init/postgres.sql. It does not create or mutate schema/data.
//
// NOTE: uses sync.Once with t.Fatalf inside Do — if the first call fails,
// subsequent calls will skip validation (known Go footgun). Acceptable here
// because test infra failure means the entire suite should abort.
func seedPostgres(t *testing.T) {
	t.Helper()
	pgSeedOnce.Do(func() {
		db, err := sql.Open("pgx", pgDSN(t))
		if err != nil {
			t.Fatalf("postgres connect failed: %v", err)
		}
		defer db.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			t.Fatalf("postgres ping failed: %v\nHint: start seeded DB: docker compose -f docker-compose.test.yml up -d", err)
		}

		requiredTables := []string{
			"public.users",
			"public.products",
			"public.orders",
			"public.order_items",
			"public.payments",
			"public.shipments",
			"public.audit_events",
			"public.internal_metrics",
			"auth.sessions",
		}

		for _, table := range requiredTables {
			var exists bool
			if err := db.QueryRowContext(
				ctx,
				`SELECT to_regclass($1) IS NOT NULL`,
				table,
			).Scan(&exists); err != nil {
				t.Fatalf("postgres table check failed for %s: %v", table, err)
			}
			if !exists {
				t.Fatalf("required seeded table missing: %s\nHint: recreate seeded DB: docker compose -f docker-compose.test.yml down -v && docker compose -f docker-compose.test.yml up -d", table)
			}
		}

		requiredRows := []string{"users", "orders", "order_items", "payments", "shipments"}
		for _, table := range requiredRows {
			var count int
			// Table names are hardcoded literals above; quote to prevent any
			// accidental injection if this pattern is copied.
			query := fmt.Sprintf(`SELECT count(*) FROM "%s"`, strings.ReplaceAll(table, `"`, `""`))
			if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
				t.Fatalf("postgres row-count check failed for %s: %v", table, err)
			}
			if count == 0 {
				t.Fatalf("required seeded table has no rows: %s\nHint: recreate seeded DB: docker compose -f docker-compose.test.yml down -v && docker compose -f docker-compose.test.yml up -d", table)
			}
		}
	})
}

// seedMongo validates that integration DB is already initialized via
// testdata/init/mongo.js. It does not create or mutate schema/data.
//
// NOTE: uses sync.Once with t.Fatalf inside Do — see seedPostgres comment.
func seedMongo(t *testing.T) {
	t.Helper()
	mongoSeedOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		client, err := mongo.Connect(options.Client().ApplyURI(mongoURI(t)))
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
		nameSet := make(map[string]bool, len(names))
		for _, n := range names {
			nameSet[n] = true
		}

		requiredCollections := []string{
			"users",
			"products",
			"orders",
			"order_items",
			"payments",
			"shipments",
			"audit_events",
			"internal_metrics",
		}
		for _, coll := range requiredCollections {
			if !nameSet[coll] {
				t.Fatalf("required seeded collection missing: %s\nHint: recreate seeded DB: docker compose -f docker-compose.test.yml down -v && docker compose -f docker-compose.test.yml up -d", coll)
			}
		}

		requiredRows := []string{"users", "orders", "order_items", "payments", "shipments"}
		for _, coll := range requiredRows {
			count, err := db.Collection(coll).CountDocuments(ctx, bson.D{})
			if err != nil {
				t.Fatalf("mongo row-count check failed for %s: %v", coll, err)
			}
			if count == 0 {
				t.Fatalf("required seeded collection has no documents: %s\nHint: recreate seeded DB: docker compose -f docker-compose.test.yml down -v && docker compose -f docker-compose.test.yml up -d", coll)
			}
		}
	})
}

func postgresExport(t *testing.T) *schema.CtxExport {
	t.Helper()
	seedPostgres(t)

	pgExportOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ext := &extract.PostgresExtractor{DSN: pgDSN(t)}
		pgExportCache, pgExportErr = ext.Extract(ctx)
	})

	if pgExportErr != nil {
		t.Fatalf("Extract failed: %v", pgExportErr)
	}
	return pgExportCache
}

func postgresExportMultiSchema(t *testing.T) *schema.CtxExport {
	t.Helper()
	seedPostgres(t)

	pgMultiExportOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ext := &extract.PostgresExtractor{
			DSN:     pgDSN(t),
			Schemas: []string{"public", "auth"},
		}
		pgMultiExport, pgMultiExportErr = ext.Extract(ctx)
	})

	if pgMultiExportErr != nil {
		t.Fatalf("Extract failed: %v", pgMultiExportErr)
	}
	return pgMultiExport
}

func mongoExport(t *testing.T) *schema.CtxExport {
	t.Helper()
	seedMongo(t)

	mongoExportOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		ext := &extract.MongoExtractor{
			URI:      mongoURI(t),
			Database: "dbdense_test",
		}
		mongoExportCache, mongoExportErr = ext.Extract(ctx)
	})

	if mongoExportErr != nil {
		t.Fatalf("Extract failed: %v", mongoExportErr)
	}
	return mongoExportCache
}
