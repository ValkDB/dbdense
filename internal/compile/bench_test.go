package compile

import (
	"testing"

	"github.com/valkdb/dbdense/internal/gen"
)

// benchExport generates a synthetic export with n tables using the standard
// signal tables. The result is deterministic (fixed seed) so benchmark
// iterations are comparable.
func benchExport(n int) *gen.Config {
	return &gen.Config{
		TotalTables:  n,
		SignalTables: gen.DefaultSignalTables(),
		Seed:         42,
	}
}

func BenchmarkCompileAll_50Tables(b *testing.B) {
	export := gen.Generate(*benchExport(50))
	c := &Compiler{Export: export}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.CompileAll()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileAll_500Tables(b *testing.B) {
	export := gen.Generate(*benchExport(500))
	c := &Compiler{Export: export}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.CompileAll()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileSubset_5of500(b *testing.B) {
	export := gen.Generate(*benchExport(500))
	c := &Compiler{Export: export}

	// Pick 5 signal tables as the subset.
	subset := []string{"users", "orders", "order_items", "products", "payments"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.CompileSubset(subset)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileLighthouse_500Tables(b *testing.B) {
	export := gen.Generate(*benchExport(500))
	c := &Compiler{Export: export}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.CompileLighthouse()
		if err != nil {
			b.Fatal(err)
		}
	}
}
