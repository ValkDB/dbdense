# Contributing to dbdense

## Build

```bash
go build ./cmd/dbdense
```

## Test

```bash
go test ./...
```

Integration tests require Docker:

```bash
docker compose -f docker-compose.test.yml up -d
go test -tags integration ./...
```

## Adding a new database backend

1. Create a new file in `internal/extract/` (e.g., `mysql.go`).
2. Implement the `Extractor` interface (`internal/extract/extractor.go`).
3. Register the backend with `extract.Register("mysql", factory)` in an `init()` function.
4. The rest of the pipeline (sidecar merge, compilation, serving) works unchanged.

## Adding a new output format

1. Implement the `Renderer` interface in `internal/compile/renderer.go`.
2. Pass the renderer to `Compiler.Renderer`. The default is `DDLRenderer`.

## Code style

- **No unsized maps**: always use `make(map[K]V, size)` with a capacity hint.
- **No unsized slices where capacity is known**: use `make([]T, 0, cap)`.
- **No magic numbers**: extract numeric literals into named constants with comments.
