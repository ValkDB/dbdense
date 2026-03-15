package extract

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/valkdb/dbdense/pkg/schema"
)

// DefaultSampleSize is the number of documents sampled per collection
// when MongoExtractor.SampleSize is not set.
const DefaultSampleSize = 100

// disconnectTimeout is the maximum time to wait for a clean MongoDB disconnect.
const disconnectTimeout = 5 * time.Second

// MongoExtractor extracts schema metadata from a MongoDB database by
// sampling documents and inspecting indexes. Pure Go, no CGO.
type MongoExtractor struct {
	URI        string
	Database   string
	SampleSize int // documents to sample per collection; defaults to DefaultSampleSize

	warnings []string
}

func init() {
	Register("mongodb", func() Extractor { return &MongoExtractor{} })
	Register("mongo", func() Extractor { return &MongoExtractor{} })
}

// Warnings returns non-fatal issues discovered during extraction.
func (m *MongoExtractor) Warnings() []string {
	return m.warnings
}

// SetDSN sets the connection URI for the MongoDB extractor.
func (m *MongoExtractor) SetDSN(dsn string) {
	m.URI = dsn
}

// SetSchemas sets the database name for the MongoDB extractor.
// Only the first element is used (MongoDB operates on a single database).
func (m *MongoExtractor) SetSchemas(schemas []string) {
	if len(schemas) > 0 {
		m.Database = schemas[0]
	}
}

// Extract connects to MongoDB and returns a ctxexport.v0 metadata document.
func (m *MongoExtractor) Extract(ctx context.Context) (*schema.CtxExport, error) {
	if m.URI == "" {
		return nil, fmt.Errorf("mongodb: URI must not be empty")
	}
	if m.Database == "" {
		return nil, fmt.Errorf("mongodb: Database must not be empty")
	}

	opts := options.Client().ApplyURI(m.URI)
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() {
		dctx, dcancel := context.WithTimeout(context.Background(), disconnectTimeout)
		defer dcancel()
		_ = client.Disconnect(dctx)
	}()

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}

	db := client.Database(m.Database)

	colls, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	sort.Strings(colls)

	sampleSize := m.SampleSize
	if sampleSize <= 0 {
		sampleSize = DefaultSampleSize
	}

	m.warnings = nil

	userColls := make([]string, 0, len(colls))
	collSet := make(map[string]bool, len(colls))
	for _, c := range colls {
		if strings.HasPrefix(c, "system.") {
			continue
		}
		userColls = append(userColls, c)
		collSet[c] = true
	}

	entities := make([]schema.Entity, 0, len(userColls))
	edges := make([]schema.Edge, 0, len(userColls))

	for _, name := range userColls {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("extraction cancelled: %w", err)
		}

		fields, err := m.inferFields(ctx, db.Collection(name), sampleSize)
		if err != nil {
			return nil, fmt.Errorf("infer fields for %s: %w", name, err)
		}

		paths, err := m.extractIndexes(ctx, db.Collection(name))
		if err != nil {
			return nil, fmt.Errorf("indexes for %s: %w", name, err)
		}

		// Infer edges only when the exact *_id base name matches a collection.
		for _, f := range fields {
			base, ok := refBase(f.Name)
			if !ok {
				continue
			}
			if collSet[base] {
				edges = append(edges, schema.Edge{
					FromEntity: name,
					FromField:  f.Name,
					ToEntity:   base,
					ToField:    "_id",
					Type:       "inferred_ref",
				})
				continue
			}

			m.warnings = append(m.warnings,
				fmt.Sprintf("mongodb: skipped inferred ref %s.%s -> %q: no matching collection",
					name, f.Name, base))
		}

		e := schema.Entity{
			Name:   name,
			Type:   "collection",
			Fields: fields,
		}
		if len(paths) > 0 {
			e.AccessPaths = paths
		}
		entities = append(entities, e)
	}

	return &schema.CtxExport{
		Version:  "ctxexport.v0",
		Entities: entities,
		Edges:    edges,
	}, nil
}

// inferFields samples random documents from a collection and builds a union of
// all observed top-level fields with their most common BSON type.
// Embedded documents (BSON type 0x03) are recursed one level to capture
// subfield names and types.
func (m *MongoExtractor) inferFields(ctx context.Context, coll *mongo.Collection, limit int) ([]schema.Field, error) {
	docCount, err := coll.CountDocuments(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("count documents in %s: %w", coll.Name(), err)
	}

	sampleSize := limit
	if docCount < int64(limit) {
		m.warnings = append(m.warnings,
			fmt.Sprintf("mongodb: collection %q has %d documents, below sample size %d; sampling all documents",
				coll.Name(), docCount, limit))
		sampleSize = int(docCount)
	}
	if sampleSize <= 0 {
		return nil, nil
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$sample", Value: bson.D{{Key: "size", Value: sampleSize}}}},
	}
	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("$sample aggregation failed (requires MongoDB 3.2+): %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	// Track field name -> BSON type name -> count.
	type typeCount struct {
		name  string
		count int
	}
	fieldTypes := make(map[string]map[string]int, 32)

	// Collect raw bytes of embedded documents for subfield inference.
	fieldSubDocs := make(map[string][]bson.Raw, 8) // subset of fields that are embedded docs

	for cursor.Next(ctx) {
		elements, err := cursor.Current.Elements()
		if err != nil {
			return nil, fmt.Errorf("parse document in %s: %w", coll.Name(), err)
		}
		for _, elem := range elements {
			name := elem.Key()
			typeByte := byte(elem.Value().Type)
			typeName := bsonTypeName(typeByte)
			if fieldTypes[name] == nil {
				fieldTypes[name] = make(map[string]int, 4)
			}
			fieldTypes[name][typeName]++

			// Capture embedded document bytes for one-level subfield inference.
			if typeByte == 0x03 {
				raw := make(bson.Raw, len(elem.Value().Value))
				copy(raw, elem.Value().Value)
				fieldSubDocs[name] = append(fieldSubDocs[name], raw)
			}
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}

	// Sort field names for deterministic output.
	names := make([]string, 0, len(fieldTypes))
	for name := range fieldTypes {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]schema.Field, 0, len(names))
	for _, name := range names {
		// Pick the most frequent type; break ties alphabetically.
		best := typeCount{}
		for t, c := range fieldTypes[name] {
			if c > best.count || (c == best.count && t < best.name) {
				best = typeCount{name: t, count: c}
			}
		}
		f := schema.Field{
			Name: name,
			Type: best.name,
			IsPK: name == "_id",
		}
		if subDocs, ok := fieldSubDocs[name]; ok && len(subDocs) > 0 {
			f.Subfields = inferSubfields(subDocs)
		}
		fields = append(fields, f)
	}
	return fields, nil
}

// inferSubfields builds a union of field names and types from raw embedded
// BSON documents. It does not recurse further (one level only).
func inferSubfields(docs []bson.Raw) []schema.Field {
	type typeCount struct {
		name  string
		count int
	}
	fieldTypes := make(map[string]map[string]int, 16)

	for _, doc := range docs {
		elements, err := doc.Elements()
		if err != nil {
			continue
		}
		for _, elem := range elements {
			name := elem.Key()
			typeName := bsonTypeName(byte(elem.Value().Type))
			if fieldTypes[name] == nil {
				fieldTypes[name] = make(map[string]int, 4)
			}
			fieldTypes[name][typeName]++
		}
	}

	names := make([]string, 0, len(fieldTypes))
	for name := range fieldTypes {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]schema.Field, 0, len(names))
	for _, name := range names {
		best := typeCount{}
		for t, c := range fieldTypes[name] {
			if c > best.count || (c == best.count && t < best.name) {
				best = typeCount{name: t, count: c}
			}
		}
		fields = append(fields, schema.Field{
			Name: name,
			Type: best.name,
		})
	}
	return fields
}

// extractIndexes lists the non-default indexes on a collection as AccessPaths.
func (m *MongoExtractor) extractIndexes(ctx context.Context, coll *mongo.Collection) ([]schema.AccessPath, error) {
	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	paths := make([]schema.AccessPath, 0, 4) // typical collection has 1-4 indexes
	for cursor.Next(ctx) {
		var idx struct {
			Name   string `bson:"name"`
			Key    bson.D `bson:"key"`
			Unique bool   `bson:"unique"`
		}
		if err := cursor.Decode(&idx); err != nil {
			return nil, fmt.Errorf("decode index in %s: %w", coll.Name(), err)
		}
		// Skip the default _id index.
		if idx.Name == "_id_" {
			continue
		}
		cols := make([]string, 0, len(idx.Key))
		for _, k := range idx.Key {
			cols = append(cols, k.Key)
		}
		paths = append(paths, schema.AccessPath{
			Name:     idx.Name,
			Columns:  cols,
			IsUnique: idx.Unique,
		})
	}
	return paths, cursor.Err()
}

// refBase extracts the exact base collection name from a field like "user_id".
// It does not singularize, pluralize, or otherwise transform the field name.
// Returns ("user", true) for "user_id" and ("", false) for all other fields.
func refBase(field string) (string, bool) {
	if field == "_id" || !strings.HasSuffix(field, "_id") {
		return "", false
	}

	base := strings.TrimSuffix(field, "_id")
	if base == "" {
		return "", false
	}
	return base, true
}

// bsonTypeName maps a BSON type byte to its human-readable name.
func bsonTypeName(t byte) string {
	switch t {
	case 0x01:
		return "double"
	case 0x02:
		return "string"
	case 0x03:
		return "object"
	case 0x04:
		return "array"
	case 0x05:
		return "binary"
	case 0x06:
		return "undefined"
	case 0x07:
		return "objectId"
	case 0x08:
		return "boolean"
	case 0x09:
		return "datetime"
	case 0x0A:
		return "null"
	case 0x0B:
		return "regex"
	case 0x10:
		return "int32"
	case 0x11:
		return "timestamp"
	case 0x12:
		return "int64"
	case 0x13:
		return "decimal128"
	case 0x7F:
		return "maxKey"
	case 0xFF:
		return "minKey"
	default:
		return "unknown"
	}
}
